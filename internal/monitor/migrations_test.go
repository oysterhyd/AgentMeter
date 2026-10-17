package monitor

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnversionedDatabaseMigrationPreservesUserDataAndRecoveryVersion(t *testing.T) {
	file := filepath.Join(t.TempDir(), "monitor.sqlite")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	// Original schema before account columns and before versioned migrations.
	_, err = db.Exec(`CREATE TABLE kv(key TEXT PRIMARY KEY,value TEXT NOT NULL);
		CREATE TABLE sessions(id TEXT PRIMARY KEY,project TEXT,origin TEXT,created TEXT);
		CREATE TABLE files(path TEXT PRIMARY KEY,offset INTEGER,state TEXT,mtime REAL);
		CREATE TABLE usage(id TEXT PRIMARY KEY,session TEXT,turn TEXT,ts TEXT,model TEXT,input INTEGER,cached INTEGER,output INTEGER,reasoning INTEGER,cache_write INTEGER,kind TEXT);
		CREATE TABLE prices(id INTEGER PRIMARY KEY,model TEXT,effective TEXT,input REAL,cached REAL,output REAL,cache_write REAL,source TEXT);
		INSERT INTO kv VALUES('settings','{"theme":"dark","clearedAt":"2026-10-01T00:00:00.000Z"}');
		INSERT INTO kv VALUES('seeded','true');
		INSERT INTO sessions VALUES('desktop','project','Codex Desktop','2026-10-07T00:00:00.000Z');
		INSERT INTO sessions VALUES('pi','project','pi · 官方 Codex','2026-10-07T00:00:00.000Z');
		INSERT INTO files VALUES('old-log',42,'{"desktop":false}',123);
		INSERT INTO usage(id,session,input,output) VALUES('response','desktop',100,20);
		INSERT INTO prices VALUES(9,'custom','1970-01-01T00:00:00.000Z',7,1,9,0,'手动设置');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(file)
		if err != nil {
			t.Fatal(err)
		}
		if num(s.mustOne("PRAGMA user_version")["user_version"]) != schemaVersion {
			t.Fatal("schema version")
		}
		if s.settings()["theme"] != "dark" || s.settings()["clearedAt"] != "2026-10-01T00:00:00.000Z" {
			t.Fatal("settings changed")
		}
		if row := s.mustOne("SELECT * FROM usage WHERE id='response'"); num(row["input"]) != 100 || row["account"] != Unknown {
			t.Fatal(row)
		}
		if row := s.mustOne("SELECT * FROM files WHERE path='old-log'"); num(row["offset"]) != 42 || num(row["parser_version"]) != 0 {
			t.Fatal(row)
		}
		if row := s.mustOne("SELECT * FROM prices WHERE id=9"); num(row["input"]) != 7 || row["source"] != "手动设置" {
			t.Fatal(row)
		}
		if s.mustOne("SELECT source FROM sessions WHERE id='desktop'")["source"] != "desktop" || s.mustOne("SELECT source FROM sessions WHERE id='pi'")["source"] != "pi" {
			t.Fatal("source backfill")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	recovered := filepath.Join(t.TempDir(), "recovered.sqlite")
	if _, err := RecoverDatabase(file, recovered); err != nil {
		t.Fatal(err)
	}
	s, err := Open(recovered)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if num(s.mustOne("PRAGMA user_version")["user_version"]) != schemaVersion {
		t.Fatal("recovery lost version")
	}
}

func TestMigrationRollsBackColumnsAndVersionOnFailure(t *testing.T) {
	s := testStore(t)
	if err := s.exec(`DROP INDEX sessions_source;
		ALTER TABLE sessions DROP COLUMN source;
		ALTER TABLE files DROP COLUMN parser_version;
		PRAGMA user_version=1;
		CREATE INDEX sessions_source ON usage(ts);`); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(s.migrate); err == nil {
		t.Fatal("expected index conflict")
	}
	if num(s.mustOne("PRAGMA user_version")["user_version"]) != 1 {
		t.Fatal("failed migration advanced version")
	}
	for _, table := range []string{"sessions", "files"} {
		for _, col := range s.mustQuery("PRAGMA table_info(" + table + ")") {
			if col["name"] == "source" || col["name"] == "parser_version" {
				t.Fatal("failed migration left column", table, col)
			}
		}
	}
}

func TestRejectFutureDatabaseSchema(t *testing.T) {
	file := filepath.Join(t.TempDir(), "monitor.sqlite")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if next, err := Open(file); err == nil {
		next.Close()
		t.Fatal("future schema admitted")
	} else if !strings.Contains(err.Error(), "999") {
		t.Fatal(err)
	}
}

func TestVSCodeSourceMigrationPreservesVersionThreeDataAndRollsBack(t *testing.T) {
	s := testStore(t)
	if err := s.exec(`DROP TABLE sessions;
		CREATE TABLE sessions(id TEXT PRIMARY KEY,project TEXT,origin TEXT,created TEXT,
		 source TEXT NOT NULL DEFAULT 'unknown' CHECK(source IN ('unknown','desktop','cli','pi')));
		CREATE INDEX sessions_source ON sessions(source,id);
		INSERT INTO sessions VALUES('old','project','Codex Desktop','2026-10-07','desktop');
		INSERT INTO usage(id,session,input,output) VALUES('kept','old',100,20);
		PRAGMA user_version=3;
		CREATE TABLE sessions_v4(conflict TEXT);`); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(s.migrate); err == nil {
		t.Fatal("expected table conflict")
	}
	if num(s.mustOne("PRAGMA user_version")["user_version"]) != 3 || s.mustOne("SELECT source FROM sessions WHERE id='old'")["source"] != "desktop" {
		t.Fatal("failed migration changed data or version")
	}
	if err := s.exec("DROP TABLE sessions_v4"); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(s.migrate); err != nil {
		t.Fatal(err)
	}
	if s.mustOne("SELECT project FROM sessions WHERE id='old'")["project"] != "project" || num(s.mustOne("SELECT input FROM usage WHERE id='kept'")["input"]) != 100 {
		t.Fatal("migration lost existing data")
	}
	if err := s.exec("INSERT INTO sessions(id,source) VALUES('new','vscode')"); err != nil {
		t.Fatal(err)
	}
	if err := s.exec("INSERT INTO sessions(id,source) VALUES('invalid','other')"); err == nil {
		t.Fatal("migration removed source validation")
	}
	if num(s.mustOne("PRAGMA user_version")["user_version"]) != schemaVersion {
		t.Fatal("migration did not advance version")
	}
}

func TestVersionTwoRequestMigrationPreservesUsageAndRollsBackFailure(t *testing.T) {
	s := testStore(t)
	if err := s.exec("INSERT INTO usage(id,input,output,account) VALUES('preserved',100,20,'a'); DROP TABLE request_events; PRAGMA user_version=2; CREATE INDEX request_events_time ON usage(ts)"); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(s.migrate); err == nil {
		t.Fatal("expected request index conflict")
	}
	if num(s.mustOne("PRAGMA user_version")["user_version"]) != 2 || len(s.mustOne("SELECT 1 FROM sqlite_master WHERE name='request_events'")) != 0 {
		t.Fatal("request migration did not roll back")
	}
	if err := s.exec("DROP INDEX request_events_time"); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(s.migrate); err != nil {
		t.Fatal(err)
	}
	if row := s.mustOne("SELECT input,output,account FROM usage WHERE id='preserved'"); num(row["input"]) != 100 || num(row["output"]) != 20 || row["account"] != "a" {
		t.Fatal(row)
	}
	if num(s.mustOne("PRAGMA user_version")["user_version"]) != schemaVersion {
		t.Fatal("schema version")
	}
}
