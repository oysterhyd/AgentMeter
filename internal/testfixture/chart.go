package testfixture

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"
)

// SeedChartHistory extends a new isolated acceptance profile with 100 days of
// hourly observations and 480 five-hour quota windows. No personal data is read.
func SeedChartHistory(dir string) error {
	if err := Seed(dir); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "monitor.sqlite"))
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	start := time.Now().Truncate(time.Hour).Add(-2400 * time.Hour)
	for i := 0; i < 2400; i++ {
		at := start.Add(time.Duration(i) * time.Hour)
		ts := at.UTC().Format("2006-01-02T15:04:05.000Z")
		if _, err = tx.Exec("INSERT INTO usage(id,session,turn,ts,model,input,cached,output,reasoning,cache_write,kind,account) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", fmt.Sprintf("chart-usage-%d", i), "native-session-0", "", ts, "gpt-6-astra", 10000+i%19*600, 5000, 1000+i%13*250, 200, 0, "逐次记录", "native-account-a"); err != nil {
			return err
		}
		reset := start.Add(time.Duration(i/5*5+5) * time.Hour).Unix()
		if _, err = tx.Exec("INSERT INTO quotas(id,ts,bucket,slot,used,minutes,resets,plan,source,account) VALUES(?,?,?,?,?,?,?,?,?,?)", fmt.Sprintf("chart-quota-%d", i), ts, "codex", "primary", i%5*23, 300, reset, "plus", "在线查询", "native-account-a"); err != nil {
			return err
		}
	}
	return tx.Commit()
}
