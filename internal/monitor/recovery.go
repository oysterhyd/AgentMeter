package monitor

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RepairProfile keeps the original database and its WAL in a timestamped
// backup directory, then installs a fully validated reconstruction.
func RepairProfile(data string) (backup string, err error) {
	release, err := AcquireProfileLock(data)
	if err != nil {
		return "", err
	}
	defer release()
	dir := filepath.Join(data, "database-backups")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	backup, err = os.MkdirTemp(dir, "recovery-"+time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return "", err
	}
	source := filepath.Join(data, "monitor.sqlite")
	recovered := filepath.Join(backup, "recovered.sqlite")
	if _, err = RecoverDatabase(source, recovered); err != nil {
		return backup, err
	}
	var moved []string
	rollback := func(cause error) error {
		for i := len(moved) - 1; i >= 0; i-- {
			suffix := moved[i]
			if e := os.Rename(filepath.Join(backup, "monitor.sqlite"+suffix), source+suffix); e != nil {
				cause = fmt.Errorf("%w；恢复原文件失败：%v", cause, e)
			}
		}
		return fmt.Errorf("%w；备份目录：%s", cause, backup)
	}
	for _, suffix := range []string{"-wal", "-shm", ""} {
		if err = os.Rename(source+suffix, filepath.Join(backup, "monitor.sqlite"+suffix)); err != nil {
			if suffix != "" && os.IsNotExist(err) {
				continue
			}
			return backup, rollback(err)
		}
		moved = append(moved, suffix)
	}
	if err = os.Rename(recovered, source); err != nil {
		return backup, rollback(err)
	}
	return backup, nil
}

// RecoverDatabase rebuilds readable tables and indexes into a new file. It
// refuses to overwrite a destination or silently discard unreadable rows.
// The caller must own the profile lock so the source and WAL stay consistent.
func RecoverDatabase(source, destination string) (counts map[string]int64, err error) {
	if _, err = os.Stat(source); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(destination)
		}
	}()
	if err = f.Close(); err != nil {
		return nil, err
	}
	// Validate and read a private snapshot so SQLite cannot modify the source's
	// shared-memory files or checkpoint its pending committed WAL.
	dir, err := os.MkdirTemp(filepath.Dir(destination), ".monitor-recovery-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	copy := filepath.Join(dir, "monitor.sqlite")
	for _, suffix := range []string{"", "-wal"} {
		input, e := os.Open(source + suffix)
		if suffix != "" && os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		output, e := os.OpenFile(copy+suffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			input.Close()
			return nil, e
		}
		_, e = io.Copy(output, input)
		input.Close()
		closeErr := output.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	absolute, err := filepath.Abs(copy)
	if err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	old := &Store{db: db}
	version, err := old.one("PRAGMA user_version")
	if err != nil {
		return nil, err
	}
	schema, err := old.query(`SELECT type,name,sql FROM sqlite_master
		WHERE sql IS NOT NULL AND substr(name,1,7) != 'sqlite_'
		ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 WHEN 'view' THEN 2 ELSE 3 END,rootpage`)
	if err != nil {
		return nil, err
	}
	if len(schema) == 0 {
		return nil, fmt.Errorf("数据库中没有可恢复的表")
	}
	newDB, err := sql.Open("sqlite", destination)
	if err != nil {
		return nil, err
	}
	defer newDB.Close()
	newDB.SetMaxOpenConns(1)
	newStore := &Store{db: newDB}
	counts = map[string]int64{}
	err = newStore.transaction(func() error {
		for _, definition := range schema {
			if definition["type"] != "table" {
				continue
			}
			name := text(definition["name"])
			if e := newStore.exec(text(definition["sql"])); e != nil {
				return e
			}
			quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
			rows, e := db.Query("SELECT * FROM " + quoted + " NOT INDEXED")
			if e != nil {
				return fmt.Errorf("恢复表 %s：%w", name, e)
			}
			e = func() error {
				defer rows.Close()
				columns, e := rows.Columns()
				if e != nil {
					return e
				}
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				insert := "INSERT INTO " + quoted + " VALUES(" + strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",") + ")"
				statement, e := newStore.tx.Prepare(insert)
				if e != nil {
					return e
				}
				defer statement.Close()
				counts[name] = 0
				for rows.Next() {
					if e = rows.Scan(pointers...); e != nil {
						return e
					}
					if _, e = statement.Exec(values...); e != nil {
						return e
					}
					counts[name]++
				}
				return rows.Err()
			}()
			if e != nil {
				return fmt.Errorf("恢复表 %s（已读取 %d 行）：%w", name, counts[name], e)
			}
		}
		for _, definition := range schema {
			if definition["type"] != "table" {
				if e := newStore.exec(text(definition["sql"])); e != nil {
					return e
				}
			}
		}
		return newStore.exec(fmt.Sprintf("PRAGMA user_version=%d", int(num(version["user_version"]))))
	})
	if err != nil {
		return nil, err
	}
	results, err := newStore.query("PRAGMA integrity_check")
	if err != nil {
		return nil, err
	}
	if len(results) != 1 || text(results[0]["integrity_check"]) != "ok" {
		return nil, fmt.Errorf("恢复后的数据库完整性检查失败：%v", results)
	}
	err = newDB.Close()
	return counts, err
}
