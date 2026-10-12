package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCallHeapMatchesFullSortAcrossFiltersAndPages(t *testing.T) {
	s := testStore(t)
	at := time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 173; i++ {
		model := "gpt-5.5"
		if i%7 == 0 {
			model = "unpriced"
		}
		id, session, turn := fmt.Sprintf("u-%03d", i), fmt.Sprintf("s-%d", i%3), fmt.Sprintf("t-%d", i%5)
		seedCall(t, s, id, session, turn, model, "a", at.Add(time.Duration(i/4)*time.Second), (i%9)*100, (i%9)*30, i%13)
		if i%4 != 0 {
			if err := s.exec("INSERT INTO request_events(id,ts,session,kind,usage_id,ttft,source) VALUES(?,?,?,'completed',?,?,'otel')", "timing-"+id, iso(at), session, id, (i%6)*250); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 19; i++ {
		kind := "retry"
		if i%2 == 0 {
			kind = "failed"
		}
		if err := s.exec("INSERT INTO request_events(id,ts,session,turn,model,kind,source,account) VALUES(?,?,?,'t-0','gpt-5.5',?,'otel','a')", fmt.Sprintf("e-%03d", i), iso(at.Add(time.Duration(i)*time.Second)), "s-0", kind); err != nil {
			t.Fatal(err)
		}
	}
	pricing := indexPrices(s.prices())
	for _, order := range []string{"", "newest", "oldest", "tokens", "cost", "ttft"} {
		for _, status := range []string{"", "usage", "failed", "retry", "missing"} {
			for _, search := range []string{"", " U-0 ", "gpt", "nonexistent"} {
				f := Object{"callOrder": order, "callStatus": status, "callSearch": search}
				rows := filterCalls(s.callRows(f, "0000", "9999", pricing), f)
				// The previous implementation sorted every matching row before pagination.
				less := callBefore(order)
				sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })
				summary := summarizeCalls(rows)
				for _, page := range []int{-1, 1, 2, 4, 999999} {
					f["callPage"] = page
					selected, pagination := paginateCalls(rows, page, 50)
					want := Object{"rows": selected, "pagination": pagination, "summary": summary}
					got := s.callSnapshot(f, "0000", "9999", pricing)
					if !equivalentJSON(got, want) {
						t.Fatalf("order=%s status=%s search=%s page=%d\ngot %s\nwant %s", order, status, search, page, jsonText(got), jsonText(want))
					}
				}
			}
		}
	}
}

// Changing aggregation order can change only the final floating-point bits.
func equivalentJSON(left, right any) bool {
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	var a, b any
	json.Unmarshal(l, &a)
	json.Unmarshal(r, &b)
	var same func(any, any) bool
	same = func(a, b any) bool {
		switch a := a.(type) {
		case float64:
			v, ok := b.(float64)
			return ok && math.Abs(a-v) <= 1e-12*math.Max(1, math.Abs(v))
		case []any:
			v, ok := b.([]any)
			if !ok || len(a) != len(v) {
				return false
			}
			for i := range a {
				if !same(a[i], v[i]) {
					return false
				}
			}
			return true
		case map[string]any:
			v, ok := b.(map[string]any)
			if !ok || len(a) != len(v) {
				return false
			}
			for k, value := range a {
				other, exists := v[k]
				if !exists || !same(value, other) {
					return false
				}
			}
			return true
		default:
			return reflect.DeepEqual(a, b)
		}
	}
	return same(a, b)
}

func TestStreamingRowsRemainIndependentAndCloseAfterPanic(t *testing.T) {
	s := testStore(t)
	s.exec("INSERT INTO accounts VALUES('a','A','manual'),('b','B','manual')")
	rows := s.mustQuery("SELECT * FROM accounts ORDER BY id")
	rows[0]["label"] = "changed"
	if rows[1]["label"] != "B" {
		t.Fatal("query rows share a map")
	}
	func() {
		defer func() { recover() }()
		s.each("SELECT * FROM accounts", nil, func(Object) { panic("stop") })
	}()
	if num(s.mustOne("SELECT COUNT(*) n FROM accounts")["n"]) != 2 {
		t.Fatal("SQL cursor was not closed")
	}
}

func TestCSVWriterPreservesExactEscaping(t *testing.T) {
	cols := []string{"a", "b"}
	rows := []Object{{"a": "=formula", "b": "a,\"b\"\r\n"}, {"a": nil, "b": "中文"}, {"a": "\tcell", "b": -2}}
	want := "\uFEFFa,b\r\n\"'=formula\",\"a,\"\"b\"\"\r\n\"\r\n\"\",\"中文\"\r\n\"'\tcell\",\"'-2\""
	if got := csvRows(cols, rows); got != want {
		t.Fatalf("%q != %q", got, want)
	}
	if got := csvRows(cols, nil); got != "\uFEFF"+strings.Join(cols, ",") {
		t.Fatal(got)
	}
}

func TestScanBoundaryIsReleasedOnCancellation(t *testing.T) {
	s := testStore(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "cancelled.jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Scan(ctx, home, "", "", true, nil); err != context.Canceled {
		t.Fatal(err)
	}
	if s.scanBoundary != nil {
		t.Fatal("cancelled scan retained its boundary")
	}
	settings := s.settings()
	settings["clearedAt"] = "2026-10-09T00:00:00.000Z"
	s.set("settings", settings)
	if s.clearedAt() != settings["clearedAt"] {
		t.Fatal("clear reused a stale scan boundary")
	}
}
