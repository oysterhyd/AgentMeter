package monitor

import "fmt"

const schemaVersion = 5
const codexParserVersion = 5
const piParserVersion = 2
const claudeParserVersion = 1

// Agents are the coding clients whose local records feed the statistics.
const (
	AgentCodex  = "codex"
	AgentClaude = "claude"
)

func (s *Store) checkSchemaVersion() error {
	row, err := s.one("PRAGMA user_version")
	if err != nil {
		return err
	}
	if num(row["user_version"]) > schemaVersion {
		return fmt.Errorf("数据库版本 %v 高于本程序支持的 %d，请升级 %s", row["user_version"], schemaVersion, AppName)
	}
	return nil
}

// Called inside initialize's transaction: columns, indexes and version commit
// together. Version zero includes every historical, unversioned schema.
func (s *Store) migrate() error {
	version := int(num(s.mustOne("PRAGMA user_version")["user_version"]))
	if version < 1 {
		if err := s.migrateLegacy(); err != nil {
			return err
		}
	}
	if version < 2 {
		if err := s.exec(`ALTER TABLE sessions ADD COLUMN source TEXT NOT NULL DEFAULT 'unknown' CHECK(source IN ('unknown','desktop','cli','pi'));
            ALTER TABLE files ADD COLUMN parser_version INTEGER NOT NULL DEFAULT 0;
            UPDATE sessions SET source='desktop' WHERE origin='Codex Desktop';
            UPDATE sessions SET source='pi' WHERE origin='pi · 官方 Codex';
            CREATE INDEX sessions_source ON sessions(source,id);`); err != nil {
			return err
		}
	}
	if version < 3 {
		if err := s.exec(`CREATE TABLE request_events(
            id TEXT PRIMARY KEY,ts TEXT NOT NULL,session TEXT NOT NULL,turn TEXT,model TEXT,
            kind TEXT NOT NULL,usage_id TEXT,response_id TEXT,input INTEGER,cached INTEGER,output INTEGER,reasoning INTEGER,
            ttft REAL,attempt INTEGER,http_status INTEGER,error_code TEXT,source TEXT NOT NULL,
            association TEXT,request_key TEXT,account TEXT NOT NULL DEFAULT 'unassigned');
            CREATE INDEX request_events_time ON request_events(ts);
            CREATE INDEX request_events_turn ON request_events(session,turn,ts);
            CREATE UNIQUE INDEX request_events_usage ON request_events(usage_id) WHERE usage_id IS NOT NULL;
            CREATE INDEX request_events_account ON request_events(account,ts);
            CREATE INDEX request_events_request_key ON request_events(session,request_key);`); err != nil {
			return err
		}
		if err := s.exec("CREATE INDEX IF NOT EXISTS usage_session_time ON usage(session,ts)"); err != nil {
			return err
		}
	}
	if version < 4 {
		// SQLite cannot extend a CHECK constraint in place. Replace only this
		// small metadata table; usage, accounts, prices and scan offsets stay put.
		if err := s.exec(`CREATE TABLE sessions_v4(id TEXT PRIMARY KEY,project TEXT,origin TEXT,created TEXT,
            source TEXT NOT NULL DEFAULT 'unknown' CHECK(source IN ('unknown','desktop','cli','vscode','pi')));
            INSERT INTO sessions_v4 SELECT id,project,origin,created,source FROM sessions;
            DROP TABLE sessions;
            ALTER TABLE sessions_v4 RENAME TO sessions;
            CREATE INDEX sessions_source ON sessions(source,id);`); err != nil {
			return err
		}
	}
	if version < 5 {
		// Every existing row came from Codex (Pi uses official Codex logins).
		// Agent is denormalized onto the hot tables so filters need no join.
		for _, change := range [][3]string{{"sessions", "agent", agentColumn}, {"usage", "agent", agentColumn}, {"usage", "cache_write_1h", "INTEGER NOT NULL DEFAULT 0"},
			{"turns", "agent", agentColumn}, {"quotas", "agent", agentColumn}, {"request_events", "agent", agentColumn}, {"prices", "cache_write_1h", "REAL"}} {
			if err := s.addColumn(change[0], change[1], change[2]); err != nil {
				return err
			}
		}
		if err := s.exec(`CREATE INDEX IF NOT EXISTS usage_agent_time ON usage(agent,ts);
            CREATE INDEX IF NOT EXISTS turns_agent_started ON turns(agent,started);
            CREATE INDEX IF NOT EXISTS turns_session_started ON turns(session,started);`); err != nil {
			return err
		}
	}
	if version < schemaVersion {
		return s.exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	}
	return nil
}

func (s *Store) migrateLegacy() error {
	var e error
	for _, change := range [][3]string{{"account_observations", "source", "TEXT NOT NULL DEFAULT 'codex'"}, {"usage", "account", "TEXT NOT NULL DEFAULT 'unassigned'"}, {"turns", "account", "TEXT NOT NULL DEFAULT 'unassigned'"}, {"quotas", "account", "TEXT NOT NULL DEFAULT 'unassigned'"}, {"prices", "retired", "INTEGER NOT NULL DEFAULT 0"}} {
		if e = s.addColumn(change[0], change[1], change[2]); e != nil {
			return e
		}
	}
	for _, t := range []string{"usage", "turns", "quotas"} {
		col := "ts"
		if t == "turns" {
			col = "started"
		}
		if e = s.exec("CREATE INDEX IF NOT EXISTS " + t + "_account_time ON " + t + "(account," + col + ")"); e != nil {
			return e
		}
	}
	if e = s.exec("CREATE INDEX IF NOT EXISTS quota_account_window_time ON quotas(account,bucket,slot,ts DESC)"); e != nil {
		return e
	}
	// Lets the model filter list skip from one distinct model to the next
	// instead of reading every usage row.
	if e = s.exec("CREATE INDEX IF NOT EXISTS usage_model_account ON usage(model,account)"); e != nil {
		return e
	}
	return nil
}

const agentColumn = "TEXT NOT NULL DEFAULT 'codex'"

// addColumn is idempotent: unversioned and rewound schemas may already have it.
func (s *Store) addColumn(table, column, definition string) error {
	cols, err := s.query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	for _, c := range cols {
		if c["name"] == column {
			return nil
		}
	}
	return s.exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
}
