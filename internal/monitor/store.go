package monitor

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Object = map[string]any

var ErrDatabaseIntegrity = errors.New("监测数据库完整性检查失败")

const Unknown = "unassigned"
const UnassignedProject = "未归属项目"
const PiOrigin = "pi · 官方 Codex"
const PiKind = "pi 官方 Codex"
const PiOpenAIKind = "pi 官方登录 · OpenAI"

func obj(v any) Object {
	if r, ok := v.(map[string]any); ok {
		return r
	}
	return Object{}
}
func text(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if n, ok := v.(float64); ok {
		if n == 0 {
			return "0"
		}
		format := byte('f')
		if math.Abs(n) < 1e-6 || math.Abs(n) >= 1e21 {
			format = 'e'
		}
		s := strconv.FormatFloat(n, format, -1, 64)
		s = strings.ReplaceAll(strings.ReplaceAll(s, "e-0", "e-"), "e+0", "e+")
		return s
	}
	return fmt.Sprint(v)
}
func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		r, _ := n.Float64()
		return r
	}
	return 0
}
func truth(v any) bool {
	if v == nil {
		return false
	}
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case int64:
		return v != 0
	}
	return true
}
func validNumber(v any) bool {
	switch v.(type) {
	case float64, int, int64, json.Number:
		return num(v) >= 0 && !math.IsNaN(num(v)) && !math.IsInf(num(v), 0)
	}
	return false
}
func clone(v Object) Object {
	r := Object{}
	for k, x := range v {
		r[k] = x
	}
	return r
}
func hash(v string) string   { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func parse(v any) time.Time {
	s := text(v)
	for _, f := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		var t time.Time
		var e error
		if strings.Contains(s, "Z") || strings.Contains(s, "+") {
			t, e = time.Parse(f, s)
		} else {
			t, e = time.ParseInLocation(f, s, time.Local)
		}
		if e == nil {
			return t
		}
	}
	return time.Time{}
}
func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func defaultSettings() Object {
	return Object{"theme": "system", "language": "zh-CN", "muted": false, "autoStart": false, "quotaInterval": 60, "codexExecutable": "", "clearedAt": nil, "telemetryEnabled": true, "telemetryPort": 4319}
}

type Store struct {
	db                     *sql.DB
	tx                     *sql.Tx
	intervals              []Object
	replay                 bool
	piOAuth                bool
	catalog                map[string]time.Time
	catalogOrder           []string
	piFiles                map[string]bool
	lastDiscovery          time.Time
	catalogHome, catalogPi string
	scanBoundary           *string
}

func Open(file string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
		return nil, e
	}
	if e := checkExistingDatabase(file); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", file)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if e = s.initialize(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}

// Reject corruption before opening a writable connection: closing the last
// writable connection can checkpoint a pending WAL even when validation failed.
func checkExistingDatabase(file string) error {
	info, e := os.Stat(file)
	if os.IsNotExist(e) || (e == nil && info.Size() == 0) {
		return nil
	}
	if e != nil {
		return e
	}
	absolute, e := filepath.Abs(file)
	if e != nil {
		return e
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		return e
	}
	defer db.Close()
	rows, e := db.Query("PRAGMA quick_check")
	if e != nil {
		return fmt.Errorf("%w：%w", ErrDatabaseIntegrity, e)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var result string
		if e = rows.Scan(&result); e != nil {
			return e
		}
		if result != "ok" {
			return fmt.Errorf("%w：%s", ErrDatabaseIntegrity, result)
		}
		count++
	}
	if e = rows.Err(); e != nil {
		return fmt.Errorf("%w：%w", ErrDatabaseIntegrity, e)
	}
	if count != 1 {
		return fmt.Errorf("%w：检查未返回有效结果", ErrDatabaseIntegrity)
	}
	return nil
}
func (s *Store) initialize() error {
	if e := s.checkSchemaVersion(); e != nil {
		return e
	}
	e := s.exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS kv(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY,offset INTEGER,state TEXT,mtime REAL);
 CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,project TEXT,origin TEXT,created TEXT);
 CREATE TABLE IF NOT EXISTS usage(id TEXT PRIMARY KEY,session TEXT,turn TEXT,ts TEXT,model TEXT,input INTEGER,cached INTEGER,output INTEGER,reasoning INTEGER,cache_write INTEGER,kind TEXT);
 CREATE INDEX IF NOT EXISTS usage_time ON usage(ts);CREATE INDEX IF NOT EXISTS usage_turn_session ON usage(turn,session);
 CREATE TABLE IF NOT EXISTS turns(id TEXT PRIMARY KEY,session TEXT,model TEXT,started TEXT,ended TEXT,status TEXT,duration REAL,ttft REAL,last_seen TEXT);
 CREATE INDEX IF NOT EXISTS turns_started ON turns(started);CREATE INDEX IF NOT EXISTS turns_active ON turns(status,last_seen);
 CREATE TABLE IF NOT EXISTS quotas(id TEXT PRIMARY KEY,ts TEXT,bucket TEXT,slot TEXT,used REAL,minutes REAL,resets REAL,plan TEXT,source TEXT);
 CREATE INDEX IF NOT EXISTS quota_time ON quotas(ts);CREATE INDEX IF NOT EXISTS quota_bucket_time ON quotas(bucket,slot,ts DESC);
 CREATE TABLE IF NOT EXISTS prices(id INTEGER PRIMARY KEY,model TEXT,effective TEXT,input REAL,cached REAL,output REAL,cache_write REAL,source TEXT);
 CREATE TABLE IF NOT EXISTS notices(id TEXT PRIMARY KEY,ts TEXT);
 CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY,label TEXT NOT NULL,kind TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS account_observations(id INTEGER PRIMARY KEY,account TEXT NOT NULL,started TEXT NOT NULL,ended TEXT NOT NULL);`)
	if e != nil {
		return e
	}
	return s.transaction(func() error {
		if e := s.migrate(); e != nil {
			return e
		}
		if !truth(s.get("seeded")) {
			for _, p := range officialPrices {
				if e = s.seedPrice(p, officialSource, "1970-01-01T00:00:00.000Z"); e != nil {
					return e
				}
			}
			if e = s.set("seeded", true); e != nil {
				return e
			}
		}
		if !truth(s.get("standard-price-correction-v1")) {
			for _, p := range officialPrices {
				old, e := s.query("SELECT id FROM prices WHERE model=? AND source=? AND input=? AND cached=? AND output=? AND cache_write=? AND retired=0", p[0], "官方标准短上下文价格 · 核对于 2026-09-09；历史按此基准估算", num(p[1])/2, num(p[2])/2, num(p[3])/2, num(p[4])/2)
				if e != nil {
					return e
				}
				for _, row := range old {
					if e = s.exec("UPDATE prices SET retired=1 WHERE id=?", row["id"]); e != nil {
						return e
					}
				}
				if len(old) > 0 {
					if e = s.seedPrice(p, officialSource, "1970-01-01T00:00:00.000Z"); e != nil {
						return e
					}
				}
			}
			if e = s.set("standard-price-correction-v1", true); e != nil {
				return e
			}
		}
		if !truth(s.get("legacy-model-prices-v1")) {
			for _, p := range officialPrices[:2] {
				r, e := s.one("SELECT 1 FROM prices WHERE model=? AND retired=0", p[0])
				if e != nil {
					return e
				}
				if r == nil {
					if e = s.seedPrice(p, "Standard 标准价 · 短上下文 · 官网核对 2026-09-10；历史按此基准估算", "1970-01-01T00:00:00.000Z"); e != nil {
						return e
					}
				}
			}
			return s.set("legacy-model-prices-v1", true)
		}
		return nil
	})
}

const officialSource = "Standard 标准价 · 短上下文 · 官网核对 2026-09-09"

var officialPrices = [][]any{{"gpt-5.5", 5., .5, 30., 0.}, {"gpt-5.4", 2.5, .25, 15., 0.}, {"gpt-6-astra", 10., 1., 50., 12.5}, {"gpt-5.6-sol", 4., .4, 20., 5.}, {"gpt-5.6-terra", 2., .2, 12., 2.5}, {"gpt-5.6-luna", .2, .02, 1.2, .25}}

func (s *Store) exec(q string, args ...any) error {
	var e error
	if s.tx != nil {
		_, e = s.tx.Exec(q, args...)
	} else {
		_, e = s.db.Exec(q, args...)
	}
	return e
}
func (s *Store) query(q string, args ...any) ([]Object, error) {
	out := []Object{}
	err := s.readRows(q, args, func(cols []string, values []any) {
		row := make(Object, len(cols))
		for i, k := range cols {
			row[k] = values[i]
		}
		out = append(out, row)
	})
	if err != nil {
		return nil, err
	}
	return out, err
}

// readRows owns the SQL cursor. Callbacks must not issue another query on the
// single connection, and must copy values they retain beyond the callback.
func (s *Store) readRows(q string, args []any, visit func([]string, []any)) error {
	var r *sql.Rows
	var e error
	if s.tx != nil {
		r, e = s.tx.Query(q, args...)
	} else {
		r, e = s.db.Query(q, args...)
	}
	if e != nil {
		return e
	}
	defer r.Close()
	cols, e := r.Columns()
	if e != nil {
		return e
	}
	values := make([]any, len(cols))
	p := make([]any, len(cols))
	for i := range values {
		p[i] = &values[i]
	}
	for r.Next() {
		if e = r.Scan(p...); e != nil {
			return e
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		visit(cols, values)
	}
	return r.Err()
}

// Aggregations borrow one row map instead of retaining every database row.
func (s *Store) each(q string, args []any, visit func(Object)) {
	var row Object
	if err := s.readRows(q, args, func(cols []string, values []any) {
		if row == nil {
			row = make(Object, len(cols))
		}
		for i, k := range cols {
			row[k] = values[i]
		}
		visit(row)
	}); err != nil {
		panic(err)
	}
}
func (s *Store) one(q string, args ...any) (Object, error) {
	r, e := s.query(q, args...)
	if e != nil || len(r) == 0 {
		return nil, e
	}
	return r[0], nil
}
func (s *Store) mustQuery(q string, args ...any) []Object {
	r, e := s.query(q, args...)
	if e != nil {
		panic(e)
	}
	return r
}
func (s *Store) mustOne(q string, args ...any) Object {
	r, e := s.one(q, args...)
	if e != nil {
		panic(e)
	}
	if r == nil {
		return Object{}
	}
	return r
}
func (s *Store) get(k string) any {
	r := s.mustOne("SELECT value FROM kv WHERE key=?", k)
	var v any
	_ = json.Unmarshal([]byte(text(r["value"])), &v)
	return v
}
func (s *Store) set(k string, v any) error {
	return s.exec("INSERT OR REPLACE INTO kv VALUES(?,?)", k, jsonText(v))
}
func (s *Store) settings() Object {
	r := defaultSettings()
	for k, v := range obj(s.get("settings")) {
		r[k] = v
	}
	return r
}

func (s *Store) clearedAt() string {
	if s.scanBoundary != nil {
		return *s.scanBoundary
	}
	return text(s.settings()["clearedAt"])
}
func (s *Store) prices() []Object {
	return s.mustQuery("SELECT * FROM prices ORDER BY effective DESC, CASE WHEN source='手动设置' THEN 1 ELSE 0 END DESC, id DESC")
}
func (s *Store) seedPrice(p []any, source, effective string) error {
	return s.exec("INSERT INTO prices(model,effective,input,cached,output,cache_write,source) VALUES(?,?,?,?,?,?,?)", p[0], effective, p[1], p[2], p[3], p[4], source)
}
func (s *Store) transaction(fn func() error) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	s.tx = tx
	defer func() { s.tx = nil; _ = tx.Rollback() }()
	if e = fn(); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Close() error {
	_, e := s.query("PRAGMA wal_checkpoint(TRUNCATE)")
	next := s.db.Close()
	if e != nil {
		return e
	}
	return next
}
