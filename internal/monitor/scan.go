package monitor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var interesting = regexp.MustCompile(`"type"\s*:\s*"(session_meta|turn_context|world_state|thread_settings_applied|token_usage_record|task_started|task_complete|turn_aborted|token_count)"`)
var interestingPi = regexp.MustCompile(`^\s*\{\s*"type"\s*:\s*"(?:session|message|usage)"`)

// Sources are the local client directories one scan reads. Empty paths
// disable that client.
type Sources struct {
	Home, PiHome, PiSessions string
	// ClaudeHome holds Claude Code's projects; ClaudeConfig is .claude.json.
	ClaudeHome, ClaudeConfig string
	// ClaudeStatusline is the sample file the statusLine bridge writes.
	ClaudeStatusline string
	// ClaudeDesktopUsage is Claude Desktop's plan-usage-history.json.
	ClaudeDesktopUsage string
}

// sourceKind selects the collector that owns a discovered file.
type sourceKind uint8

const (
	kindCodex sourceKind = iota
	kindPi
	kindClaude
)

// errRecord marks one unreadable record. The file continues; the record is
// reported, and missing evidence must never trigger a deletion.
var errRecord = errors.New("record")

// A collector parses one client's append-only JSONL. match is a cheap
// prefilter on the raw line; process sees only lines that pass it.
type collector struct {
	version int
	match   func(line []byte) bool
	process func(s *Store, line []byte, state Object) error
}

var collectors = [...]collector{
	kindCodex: {codexParserVersion, func(line []byte) bool { return interesting.Match(line[:min(180, len(line))]) }, (*Store).processCodexLine},
	kindPi:    {piParserVersion, func(line []byte) bool { return interestingPi.Match(line[:min(180, len(line))]) }, (*Store).processPiLine},
	kindClaude: {claudeParserVersion, func(line []byte) bool {
		// Only prompts, responses and turn timings matter. Attachments, file
		// snapshots and request blobs are most of the bytes; skip them unparsed.
		return bytes.Contains(line, []byte(`"type":"assistant"`)) || bytes.Contains(line, []byte(`"type":"user"`)) || bytes.Contains(line, []byte(`"subtype":"turn_duration"`))
	}, (*Store).processClaude},
}

func (s *Store) processCodexLine(line []byte, state Object) error {
	var record Object
	if json.Unmarshal(line, &record) != nil {
		return errRecord
	}
	return s.process(record, state, line)
}

func (s *Store) processPiLine(line []byte, state Object) error {
	var record Object
	if json.Unmarshal(line, &record) != nil {
		return errRecord
	}
	return s.processPi(record, state)
}

// Scan reads Codex and Pi only; ScanSources also reads Claude Code.
func (s *Store) Scan(ctx context.Context, home, piHome, piSessions string, full bool, progress func(Object)) (Object, error) {
	return s.ScanSources(ctx, Sources{Home: home, PiHome: piHome, PiSessions: piSessions}, full, progress)
}

func (s *Store) ScanSources(ctx context.Context, src Sources, full bool, progress func(Object)) (Object, error) {
	cleared := s.clearedAt()
	s.scanBoundary = &cleared
	defer func() { s.scanBoundary = nil }()
	now := time.Now()
	stamp := iso(now)
	if e := s.observe(src.Home, stamp, "codex"); e != nil {
		return nil, e
	}
	s.piOAuth = src.PiHome != "" && piOpenAIAuth(src.PiHome)
	if src.PiHome != "" {
		for _, source := range []string{"pi", "pi-openai"} {
			if e := s.observe(src.PiHome, stamp, source); e != nil {
				return nil, e
			}
		}
	}
	claude := src.ClaudeHome != "" && truth(s.settings()["claudeEnabled"])
	if claude {
		if e := s.observeClaude(src.ClaudeConfig, src.ClaudeStatusline, src.ClaudeDesktopUsage, stamp); e != nil {
			return nil, e
		}
	}
	s.replay = truth(s.get("replayRecovered"))
	defer func() { s.replay = false }()
	full = full || s.catalog == nil || s.catalogSources != src || now.Sub(s.lastDiscovery) >= time.Minute
	if full {
		s.catalog = map[string]time.Time{}
		s.catalogOrder = []string{}
		s.kinds = map[string]sourceKind{}
		s.catalogSources = src
	}
	var walk func(string, sourceKind) error
	walk = func(dir string, kind sourceKind) error {
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
				if e = walk(file, kind); e != nil {
					return e
				}
			} else if entry.Type().IsRegular() && filepath.Ext(file) == ".jsonl" {
				if _, ok := s.catalog[file]; !ok {
					s.catalog[file] = now
					s.catalogOrder = append(s.catalogOrder, file)
				}
				if kind != kindCodex {
					s.kinds[file] = kind
				}
			}
		}
		return nil
	}
	if full {
		if e := walk(filepath.Join(src.Home, "sessions"), kindCodex); e != nil {
			return nil, e
		}
		if e := walk(filepath.Join(src.Home, "archived_sessions"), kindCodex); e != nil {
			return nil, e
		}
		s.lastDiscovery = now
	} else {
		for d := 0; d < 2; d++ {
			t := now.Add(-time.Duration(d) * 24 * time.Hour)
			for _, loc := range []*time.Location{time.Local, time.UTC} {
				t = t.In(loc)
				if e := walk(filepath.Join(src.Home, "sessions", fmt.Sprintf("%04d", t.Year()), fmt.Sprintf("%02d", t.Month()), fmt.Sprintf("%02d", t.Day())), kindCodex); e != nil {
					return nil, e
				}
			}
		}
	}
	if src.PiSessions != "" {
		if e := walk(src.PiSessions, kindPi); e != nil {
			return nil, e
		}
	}
	if claude {
		if e := walk(filepath.Join(src.ClaudeHome, "projects"), kindClaude); e != nil {
			return nil, e
		}
	}
	// Subagent transcripts are attributed to their parent session's task, so
	// they follow the main transcripts within each scan.
	files, subagents := []string{}, []string{}
	for _, file := range s.catalogOrder {
		modified, ok := s.catalog[file]
		kind := s.kinds[file]
		if !ok || !(full || kind != kindCodex || now.Sub(modified) < 2*time.Minute) {
			continue
		}
		if kind == kindClaude && strings.Contains(filepath.ToSlash(file), "/subagents/") {
			subagents = append(subagents, file)
		} else {
			files = append(files, file)
		}
	}
	files = append(files, subagents...)
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
		kind := s.kinds[file]
		c := collectors[kind]
		mtime := float64(st.ModTime().UnixNano()) / 1e6
		// Unchanged files skip the database lookup; every scan revisits Pi
		// and Claude Code transcripts, so this keeps idle scans cheap.
		if seen, ok := s.seen[file]; ok && seen.size == st.Size() && seen.mtime == mtime {
			scanned++
			continue
		}
		old := s.mustOne("SELECT * FROM files WHERE path=?", file)
		reparse := len(old) > 0 && num(old["parser_version"]) != float64(c.version)
		if !reparse && len(old) > 0 && num(old["offset"]) == float64(st.Size()) && abs(num(old["mtime"])-mtime) < .001 {
			s.remember(file, st.Size(), mtime)
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
		if kind == kindCodex && state["source"] == "" && offset > 0 {
			if e = s.exec("UPDATE files SET offset=?,mtime=? WHERE path=?", st.Size(), mtime, file); e != nil {
				return nil, e
			}
			s.remember(file, st.Size(), mtime)
			scanned++
			continue
		}
		e = s.transaction(func() error {
			if reparse && kind == kindCodex {
				s.legacyReplay = &legacyUsageReplay{existing: map[string]map[string]bool{}, retained: map[string]bool{}, suppressed: map[string]Object{}}
				defer func() { s.legacyReplay = nil }()
			}
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
				if !c.match(line) {
					continue
				}
				if e = c.process(s, line, state); e == errRecord {
					report("record_parse", file)
					// Missing source evidence must never trigger a deletion.
					s.legacyReplay = nil
					continue
				}
				if e != nil {
					return e // Roll back the file offset as well as its statistics.
				}
			}
			if s.legacyReplay != nil {
				if e = s.reconcileLegacyUsage(); e != nil {
					return e
				}
			}
			return s.exec("INSERT OR REPLACE INTO files(path,offset,state,mtime,parser_version) VALUES(?,?,?,?,?)", file, offset, jsonText(state), mtime, c.version)
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
			delete(s.seen, file)
			report("file_read", file)
		} else if offset == st.Size() {
			s.remember(file, st.Size(), mtime)
		}
		scanned++
		if scanned%10 == 0 && progress != nil {
			progress(Object{"scanned": scanned, "total": len(files)})
		}
	}
	if e := s.attributeAccounts(src.PiHome != "", claude); e != nil {
		return nil, e
	}
	diagnosticCount, diagnosticStatus := s.scanRequestDiagnostics(ctx, src.Home)
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
	piFiles, claudeFiles := 0, 0
	for _, kind := range s.kinds {
		switch kind {
		case kindPi:
			piFiles++
		case kindClaude:
			claudeFiles++
		}
	}
	claudeExists := claude && exists(filepath.Join(src.ClaudeHome, "projects"))
	status := Object{"scanned": scanned, "total": len(s.catalog), "checked": len(files), "full": full, "changed": changed, "diagnostics": diagnostics, "errors": errors, "lastScan": iso(time.Now()),
		"sourceExists": exists(filepath.Join(src.Home, "sessions")) || exists(src.PiSessions) || claudeExists, "codexSourceExists": exists(filepath.Join(src.Home, "sessions")),
		"piSourceExists": exists(src.PiSessions), "piFiles": piFiles, "piOpenaiOAuth": s.piOAuth, "claudeSourceExists": claudeExists, "claudeFiles": claudeFiles}
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

type fileStamp struct {
	size  int64
	mtime float64
}

// remember records a fully consumed file so an unchanged one is skipped.
func (s *Store) remember(file string, size int64, mtime float64) {
	if s.seen == nil {
		s.seen = map[string]fileStamp{}
	}
	s.seen[file] = fileStamp{size, mtime}
}

// attributeAccounts assigns unattributed records inside each source's latest
// observation window to the account then signed in. Each source owns only its
// own client's rows: Codex excludes Pi and Claude Code, and so on.
func (s *Store) attributeAccounts(pi, claude bool) error {
	type rule struct {
		source, agent string
		pi            bool   // owns Pi sessions rather than excluding them
		kind          string // Pi's provider kind, when the source is Pi
	}
	rules := []rule{{source: "codex", agent: AgentCodex}}
	if pi {
		rules = append(rules, rule{"pi", AgentCodex, true, PiKind}, rule{"pi-openai", AgentCodex, true, PiOpenAIKind})
	}
	if claude {
		rules = append(rules, rule{source: "claude", agent: AgentClaude})
	}
	for _, r := range rules {
		o := s.mustOne("SELECT * FROM account_observations WHERE source=? ORDER BY id DESC LIMIT 1", r.source)
		if len(o) == 0 || o["account"] == Unknown {
			continue
		}
		for _, table := range []string{"usage", "turns", "quotas", "request_events"} {
			if r.pi && (table == "quotas" || table == "request_events") || r.agent == AgentClaude && table == "request_events" {
				continue
			}
			col := "ts"
			if table == "turns" {
				col = "started"
			}
			scope := " AND agent=?"
			args := []any{o["account"], Unknown, o["started"], o["ended"], r.agent}
			if table != "quotas" && r.agent == AgentCodex {
				not := "NOT "
				if r.pi {
					not = ""
				}
				scope += " AND " + not + "EXISTS (SELECT 1 FROM sessions s WHERE s.id=" + table + ".session AND s.origin=?)"
				args = append(args, PiOrigin)
				if table == "request_events" {
					scope += " AND substr(session,1,3)!='pi:'"
				}
			}
			if r.pi {
				if table == "usage" {
					scope += " AND kind=?"
				} else {
					scope += " AND EXISTS (SELECT 1 FROM usage u WHERE u.turn=turns.id AND u.session=turns.session AND u.kind=?)"
				}
				args = append(args, r.kind)
			}
			if e := s.exec("UPDATE "+table+" SET account=? WHERE account=? AND "+col+">=? AND "+col+"<=?"+scope, args...); e != nil {
				return e
			}
		}
	}
	return nil
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
