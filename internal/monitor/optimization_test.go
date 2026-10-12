package monitor

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"testing"
	"time"
)

// Map-shaped results are retained only for the old linear-reference tests and
// the historical lookup benchmark; production aggregation uses usageCost.
func (index priceIndex) cost(r Object) Object {
	result := Object{}
	index.calculate(r).write(result)
	return result
}

func pricedCost(r, p Object) Object {
	result := Object{}
	calculateCost(r, p).write(result)
	return result
}

// Reference linear lookup from v2.6.0, kept only for equivalence tests.
func costOf(r Object, prices []Object) Object {
	for _, p := range prices {
		if !truth(p["retired"]) && p["model"] == r["model"] && text(p["effective"]) <= text(r["ts"]) {
			return pricedCost(r, p)
		}
	}
	return pricedCost(r, nil)
}

func TestActivityReusePreservesBoundsFiltersAndFutureDays(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	for i, stamp := range []time.Time{now.AddDate(-1, 0, 0), now.AddDate(0, 0, -1), now, now.AddDate(0, 0, 1)} {
		if err := s.exec("INSERT INTO usage(id,ts,model,input,output,session,account) VALUES(?,?,?,?,?,?,?)", text(i), iso(stamp), "gpt-5", 100, 30, "s", Unknown); err != nil {
			t.Fatal(err)
		}
	}
	for _, year := range []int{2025, 2026, 1999} {
		filter := Object{"page": "overview", "range": "all", "activityYear": year, "model": "gpt-5"}
		got, err := s.Snapshot(filter, now)
		if err != nil {
			t.Fatal(err)
		}
		want := s.activity(filter, indexPrices(s.prices()), now, nil)
		if !reflect.DeepEqual(got["activity"], want) {
			t.Fatalf("%d reused activity differs", year)
		}
	}
}

func TestWidgetBucketPreservesPrimaryAndCodexPriority(t *testing.T) {
	rows := []Object{{"bucket": "z", "minutes": 300}, {"bucket": "a", "minutes": 10080}, {"bucket": "codex", "minutes": 10080}}
	if widgetBucket(rows) != "z" {
		t.Fatal("primary window priority")
	}
	rows = append(rows, Object{"bucket": "codex", "minutes": 300})
	if widgetBucket(rows) != "codex" {
		t.Fatal("codex primary priority")
	}
	if widgetBucket(nil) != nil {
		t.Fatal("missing quota invented a bucket")
	}
}

func TestPriceIndexPreservesTiesRetirementAndUnknown(t *testing.T) {
	prices := []Object{
		{"model": "a", "effective": "2026-10-08", "id": 5, "retired": 1},
		{"model": "a", "effective": "2026-10-07", "id": 4, "input": 2.},
		{"model": "b", "effective": "2026-10-07", "id": 3, "input": 9.},
		{"model": "a", "effective": "2026-10-07", "id": 2, "input": 1.},
		{"model": "a", "effective": "2026-10-01", "id": 1, "input": .5},
	}
	index := indexPrices(prices)
	for _, model := range []string{"a", "b", "missing"} {
		for _, ts := range []string{"2026-09-30", "2026-10-01", "2026-10-06", "2026-10-07", "2026-10-09"} {
			row := Object{"model": model, "ts": ts, "input": 1000., "cached": 300., "cache_write": 100., "output": 200.}
			if got, want := index.cost(row), costOf(row, prices); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s: %v != %v", model, ts, got, want)
			}
		}
	}
}

func TestScanLineHandlesBorrowedOversizedAndPartialLines(t *testing.T) {
	data := append([]byte("short\n"), bytes.Repeat([]byte("x"), 1000)...)
	data = append(data, []byte("\nnext\npartial")...)
	reader := bufio.NewReaderSize(bytes.NewReader(data), 64)
	var scratch []byte
	for _, want := range []string{"short\n", string(bytes.Repeat([]byte("x"), 1000)) + "\n", "next\n"} {
		got, err := readScanLine(reader, &scratch)
		if err != nil || string(got) != want {
			t.Fatalf("line %d, %v", len(got), err)
		}
	}
	if _, err := readScanLine(reader, &scratch); err != io.EOF {
		t.Fatal(err)
	}
}
