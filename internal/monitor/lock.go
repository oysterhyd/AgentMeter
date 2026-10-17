package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
)

func AcquireProfileLock(data string) (func(), error) {
	if e := os.MkdirAll(data, 0700); e != nil {
		return nil, e
	}
	file := filepath.Join(data, "monitor-owner.json")
	nonce := uuid.NewString()
	for attempt := 0; attempt < 3; attempt++ {
		f, e := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e == nil {
			_, e = f.WriteString(jsonText(Object{"pid": os.Getpid(), "nonce": nonce}))
			_ = f.Close()
			if e != nil {
				_ = os.Remove(file)
				return nil, e
			}
			return func() {
				b, _ := os.ReadFile(file)
				var p Object
				_ = json.Unmarshal(b, &p)
				if p["nonce"] == nonce {
					_ = os.Remove(file)
				}
			}, nil
		}
		if !os.IsExist(e) {
			return nil, e
		}
		b, e := os.ReadFile(file)
		if e != nil {
			return nil, e
		}
		var p Object
		if json.Unmarshal(b, &p) != nil || num(p["pid"]) <= 0 || processAlive(int(num(p["pid"]))) {
			return nil, fmt.Errorf("另一个 %s 正在使用此数据目录，请先退出", AppName)
		}
		current, e := os.ReadFile(file)
		if e != nil || !bytes.Equal(current, b) {
			continue
		}
		_ = os.Remove(file)
	}
	return nil, fmt.Errorf("无法取得监测数据目录占用锁")
}
