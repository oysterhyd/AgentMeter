package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var interesting = regexp.MustCompile(`"type"\s*:\s*"(session_meta|turn_context|token_usage_record|task_started|task_complete|turn_aborted|token_count)"`)
var interestingPi = regexp.MustCompile(`^\s*\{\s*"type"\s*:\s*"(?:session|message|usage)"`)

func (s *Store) Scan(ctx context.Context, home, piHome, piSessions string, full bool, progress func(Object)) (Object, error) {
	cleared := s.clearedAt()
	s.scanBoundary = &cleared
	defer func() { s.scanBoundary = nil }()
	now := time.Now()
	stamp := iso(now)
	if e := s.observe(home, stamp, "codex"); e != nil {
		return nil, e
	}
	s.piOAuth = piHome != "" && piOpenAIAuth(piHome)
	if piHome != "" {
		for _, src := range []string{"pi", "pi-openai"} {
			if e := s.observe(piHome, stamp, src); e != nil {
				return nil, e
			}
		}
	}
	s.replay = truth(s.get("replayRecovered"))
	defer func() { s.replay = false }()
	full = full || s.catalog == nil || s.catalogHome != home || s.catalogPi != piSessions || now.Sub(s.lastDiscovery) >= time.Minute
	if full {
		s.catalog = map[string]time.Time{}
		s.catalogOrder = []string{}
		s.piFiles = map[string]bool{}
		s.catalogHome, s.catalogPi = home, piSessions
	}
	var walk func(string, bool) error
	walk = func(dir string, pi bool) error {
		entries, e := os.ReadDir(dir)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		for _, entry := range entries {
			file := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if e = walk(file, pi); e != nil {
					return e
				}
			} else if entry.Type().IsRegular() && filepath.Ext(file) == ".jsonl" {
				if _, ok := s.catalog[file]; !ok {
					s.catalog[file] = now
					s.catalogOrder = append(s.catalogOrder, file)
				}
				if pi {
					s.piFiles[file] = true
				}
			}
		}
		return nil
	}
	if full {
		if e := walk(filepath.Join(home, "sessions"), false); e != nil {
			return nil, e
		}
		if e := walk(filepath.Join(home, "archived_sessions"), false); e != nil {
			return nil, e
		}
		s.lastDiscovery = now
	} else {
		for d := 0; d < 2; d++ {
			t := now.Add(-time.Duration(d) * 24 * time.Hour)
			for _, loc := range []*time.Location{time.Local, time.UTC} {
				t = t.In(loc)
				if e := walk(filepath.Join(home, "sessions", fmt.Sprintf("%04d", t.Year()), fmt.Sprintf("%02d", t.Month()), fmt.Sprintf("%02d", t.Day())), false); e != nil {
					return nil, e
				}
			}
		}
	}
	if piSessions != "" {
		if e := walk(piSessions, true); e != nil {
			return nil, e
		}
	}
	files := []string{}
	for _, file := range s.catalogOrder {
		modified, ok := s.catalog[file]
		if ok && (full || s.piFiles[file] || now.Sub(modified) < 2*time.Minute) {
			files = append(files, file)
		}
	}
	changed := false
	errors, scanned := 0, 0
	diagnostics := []Object{}
	report := func(code, file string) {
		errors++
		if len(diagnostics) < 20 {
			diagnostics = append(diagnostics, Object{"code": code, "fileId": hash(file)[:12]})
		}
	}
	var reader *bufio.Reader
	var scratch []byte
	for _, file := range files {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		st, e := os.Stat(file)
		if e != nil {
			report("file_stat", file)
			delete(s.catalog, file)
			continue
		}
		s.catalog[file] = st.ModTime()
		old := s.mustOne("SELECT * FROM files WHERE path=?", file)
		pi := s.piFiles[file]
		version := codexParserVersion
		if pi {
			version = piParserVersion
		}
		reparse := len(old) > 0 && num(old["parser_version"]) != float64(version)
		mtime := float64(st.ModTime().UnixNano()) / 1e6
		if !reparse && len(old) > 0 && num(old["offset"]) == float64(st.Size()) && abs(num(old["mtime"])-mtime) < .001 {
			scanned++
			continue
		}
		changed = true
		offset := int64(0)
		if !reparse && num(old["offset"]) <= float64(st.Size()) {
			offset = int64(num(old["offset"]))
		}
		state := Object{}
		if offset > 0 {
			_ = json.Unmarshal([]byte(text(old["state"])), &state)
		}
		if !pi && state["source"] == "" && offset > 0 {
			if e = s.exec("UPDATE files SET offset=?,mtime=? WHERE path=?", st.Size(), mtime, file); e != nil {
				return nil, e
			}
			scanned++
			continue
		}
		e = s.transaction(func() error {
			input, e := os.Open(file)
			if e != nil {
				return e
			}
			defer input.Close()
			if _, e = input.Seek(offset, io.SeekStart); e != nil {
				return e
			}
			if reader == nil {
				reader = bufio.NewReaderSize(input, 256*1024)
			} else {
				reader.Reset(input)
			}
			for {
				if e = ctx.Err(); e != nil {
					return e
				}
				line, next := readScanLine(reader, &scratch)
				if next == io.EOF {
					break
				}
				if next != nil {
					return next
				}
				offset += int64(len(line))
				head := line[:min(180, len(line))]
				wanted := interesting.Match(head)
				if pi {
					wanted = interestingPi.Match(head)
				}
				if !wanted {
					continue
				}
				var record Object
				if json.Unmarshal(line, &record) != nil {
					report("record_parse", file)
					continue
				}
				if pi {
					e = s.processPi(record, state)
				} else {
					e = s.process(record, state, line)
				}
				if e != nil {
					return e // Roll back the file offset as well as its statistics.
				}
			}
			return s.exec("INSERT OR REPLACE INTO files(path,offset,state,mtime,parser_version) VALUES(?,?,?,?,?)", file, offset, jsonText(state), mtime, version)
		})
		// Reuse ordinary long-line storage, but release exceptional payloads
		// before scanning the remaining files.
		if cap(scratch) > 1<<20 {
			scratch = nil
		}
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			report("file_read", file)
		}
		scanned++
		if scanned%10 == 0 && progress != nil {
			progress(Object{"scanned": scanned, "total": len(files)})
		}
	}
	sources := []string{"codex"}
	if piHome != "" {
		sources = append(sources, "pi", "pi-openai")
	}
	for _, source := range sources {
		o := s.mustOne("SELECT * FROM account_observations WHERE source=? ORDER BY id DESC LIMIT 1", source)
		if len(o) == 0 || o["account"] == Unknown {
			continue
		}
		for _, t := range []string{"usage", "turns", "quotas", "request_events"} {
			piSource := source != "codex"
			if piSource && (t == "quotas" || t == "request_events") {
				continue
			}
			col := "ts"
			if t == "turns" {
				col = "started"
			}
			scope := ""
			args := []any{o["account"], Unknown, o["started"], o["ended"]}
			if t != "quotas" {
				not := "NOT "
				if piSource {
					not = ""
				}
				scope = " AND " + not + "EXISTS (SELECT 1 FROM sessions s WHERE s.id=" + t + ".session AND s.origin=?)"
				args = append(args, PiOrigin)
				if t == "request_events" {
					scope += " AND substr(session,1,3)!='pi:'"
				}
			}
			if piSource {
				kind := PiKind
				if source == "pi-openai" {
					kind = PiOpenAIKind
				}
				if t == "usage" {
					scope += " AND kind=?"
				} else {
					scope += " AND EXISTS (SELECT 1 FROM usage u WHERE u.turn=turns.id AND u.session=turns.session AND u.kind=?)"
				}
				args = append(args, kind)
			}
			if e := s.exec("UPDATE "+t+" SET account=? WHERE account=? AND "+col+">=? AND "+col+"<=?"+scope, args...); e != nil {
				return nil, e
			}
		}
	}
	diagnosticCount, diagnosticStatus := s.scanRequestDiagnostics(ctx, home)
	if e := s.set("requestDiagnostics", diagnosticStatus); e != nil {
		return nil, e
	}
	if diagnosticCount > 0 {
		changed = true
	}
	if changed {
		if e := s.reconcileRequestEvents(); e != nil {
			return nil, e
		}
	}
	exists := func(p string) bool { _, e := os.Stat(p); return p != "" && e == nil }
	status := Object{"scanned": scanned, "total": len(s.catalog), "checked": len(files), "full": full, "changed": changed, "diagnostics": diagnostics, "errors": errors, "lastScan": iso(time.Now()), "sourceExists": exists(filepath.Join(home, "sessions")) || exists(piSessions), "piSourceExists": exists(piSessions), "piFiles": len(s.piFiles), "piOpenaiOAuth": s.piOAuth}
	if e := s.set("scan", status); e != nil {
		return nil, e
	}
	if s.replay && errors == 0 {
		if e := s.set("replayRecovered", false); e != nil {
			return nil, e
		}
	}
	return status, nil
}

// Borrow normal lines from the reader. Only an oversized line needs scratch
// space, which is reused within this file. EOF's incomplete line is left for
// the next scan, preserving the committed offset of append-only JSONL files.
func readScanLine(reader *bufio.Reader, scratch *[]byte) ([]byte, error) {
	*scratch = (*scratch)[:0]
	for {
		part, err := reader.ReadSlice('\n')
		if len(*scratch) == 0 && err != bufio.ErrBufferFull {
			return part, err
		}
		*scratch = append(*scratch, part...)
		if err != bufio.ErrBufferFull {
			return *scratch, err
		}
	}
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
