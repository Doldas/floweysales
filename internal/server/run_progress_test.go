package server

import (
	"strconv"
	"testing"
	"time"
)

func TestRunProgressIgnoresBadIDsAndExpires(t *testing.T) {
	tb := newRunProgressTable()
	now := time.Unix(1000, 0)
	tb.now = func() time.Time { return now }
	tb.set("bad id!", "x", 1, 2, false)
	if _, ok := tb.get("bad id!"); ok {
		t.Fatal("an invalid id must not be stored")
	}
	tb.set("run-12345678", "Backtesting", 1, 4, false)
	if p, ok := tb.get("run-12345678"); !ok || p.Done != 1 || p.Total != 4 || p.Finished {
		t.Fatalf("progress: %+v %v", p, ok)
	}
	now = now.Add(runProgressTTL + time.Second)
	if _, ok := tb.get("run-12345678"); ok {
		t.Fatal("an old entry must expire")
	}
}

func TestRunProgressTableIsBounded(t *testing.T) {
	tb := newRunProgressTable()
	for i := 0; i < runProgressMax+50; i++ {
		tb.set("run-"+strconv.Itoa(i)+"-xxxx", "s", 0, 1, false)
	}
	if len(tb.entries) > runProgressMax {
		t.Fatalf("table grew to %d", len(tb.entries))
	}
}
