package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getSensitivity(t *testing.T, query string) (int, sensitivityResponse, map[string]any) {
	t.Helper()
	e := backtestEngine(t)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest/sensitivity?"+query, nil))
	var res sensitivityResponse
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	return rec.Code, res, raw
}

func TestSensitivityChangesOneSettingAtATime(t *testing.T) {
	code, res, raw := getSensitivity(t, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&cost_bps=10&costs=0,10,50&tops=1,2&slippages=0&delays=0")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, raw)
	}
	var got []string
	for _, v := range res.Variants {
		if v.Metrics == nil {
			t.Fatalf("variant without metrics: %+v", v)
		}
		got = append(got, v.Parameter+"="+v.Value)
	}
	want := "base=as asked,cost_bps=0,cost_bps=50,top=2,rebalance=quarterly"
	if strings.Join(got, ",") != want {
		t.Fatalf("variants %v, want %s", got, want)
	}
	if res.Summary.Completed != 5 || res.Summary.CAGRPct == nil || res.Summary.WithBenchmark != 5 {
		t.Fatalf("summary %+v", res.Summary)
	}
	// More trading cost can only lower the return of the same picks.
	var c0, c50 float64
	for _, v := range res.Variants {
		switch v.Parameter + v.Value {
		case "cost_bps0":
			c0 = v.Metrics.CAGRPct
		case "cost_bps50":
			c50 = v.Metrics.CAGRPct
		}
	}
	if c50 > c0 {
		t.Errorf("50 bps (%.2f) should not beat 0 bps (%.2f)", c50, c0)
	}
	if res.Base["top"] != "1" || res.Base["rebalance"] != "monthly" {
		t.Fatalf("base %v", res.Base)
	}
}

func TestSensitivitySkipsHoldingCountsTheUniverseCannotFill(t *testing.T) {
	_, res, _ := getSensitivity(t, "symbols=UP,FLAT&strategy=momentum&top=1&costs=10&tops=5,15")
	if !strings.Contains(strings.Join(res.Summary.Notes, " "), "2 holdings counts were skipped") {
		t.Fatalf("notes %v", res.Summary.Notes)
	}
	for _, v := range res.Variants {
		if v.Parameter == "top" {
			t.Errorf("unfillable top ran: %+v", v)
		}
	}
}

func TestSensitivityRefusesBadSettings(t *testing.T) {
	for _, q := range []string{"symbols=UP,FLAT&top=1&costs=a", "symbols=UP,FLAT&top=1&costs=1,2,3,4,5,6,7", "symbols=UP,FLAT&top=1&strategy=nope"} {
		if code, _, raw := getSensitivity(t, q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d %v", q, code, raw)
		}
	}
}

func TestSensitivityTestsSlippageAndFillDelay(t *testing.T) {
	code, res, raw := getSensitivity(t, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&costs=10&tops=1&slippages=0,25&delays=0,2")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, raw)
	}
	got := map[string]float64{}
	for _, v := range res.Variants {
		if v.Metrics == nil {
			t.Fatalf("variant without metrics: %+v", v)
		}
		got[v.Parameter+"="+v.Value] = v.Metrics.CAGRPct
	}
	if _, ok := got["slippage_bps=25"]; !ok {
		t.Fatalf("no slippage variant: %v", got)
	}
	if _, ok := got["delay_days=2"]; !ok {
		t.Fatalf("no delay variant: %v", got)
	}
	if got["slippage_bps=25"] > got["base=as asked"] {
		t.Errorf("slippage cannot raise the return: %v", got)
	}
	if res.Base["delay_days"] != "0" || res.Base["slippage_bps"] != "0" {
		t.Fatalf("base %v", res.Base)
	}
}
