package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getBatch(t *testing.T, query string) (int, batchResponse, map[string]any) {
	t.Helper()
	e := backtestEngine(t)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest/batch?"+query, nil))
	var ok batchResponse
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ok)
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	return rec.Code, ok, raw
}

func TestBatchRunsEachPeriodAndSummarisesTheSpread(t *testing.T) {
	code, res, raw := getBatch(t, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&periods=3,2023-01-01..")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, raw)
	}
	if len(res.Cells) != 2 || res.Summary.Completed != 2 || res.Summary.Failed != 0 {
		t.Fatalf("cells: %+v", res.Cells)
	}
	if res.Cells[0].Universe != "custom" || res.Cells[0].Period != "3" || res.Cells[1].Period != "2023-01-01.." {
		t.Fatalf("cells: %+v", res.Cells)
	}
	for _, c := range res.Cells {
		if c.Metrics == nil || c.Rebalance == 0 {
			t.Fatalf("a cell should carry its metrics: %+v", c)
		}
	}
	s := res.Summary
	if s.CAGRPct == nil || s.CAGRPct.Min > s.CAGRPct.Median || s.CAGRPct.Median > s.CAGRPct.Max {
		t.Fatalf("spread: %+v", s.CAGRPct)
	}
	if s.WithBenchmark != 2 || s.Weakest == "" || s.Strongest == "" {
		t.Fatalf("summary: %+v", s)
	}
	if len(s.Limitations) < 2 || !strings.Contains(s.Limitations[0], "overlap") {
		t.Fatalf("limitations: %v", s.Limitations)
	}
	if strings.Contains(string(mustJSON(t, res)), `"curve"`) {
		t.Error("a batch must not carry the equity curves of every cell")
	}
}

func TestBatchKeepsAFailedCellAndSaysSo(t *testing.T) {
	_, res, _ := getBatch(t, "symbols=UP,FLAT&strategy=momentum&top=1&periods=3,7")
	if res.Summary.Completed != 1 || res.Summary.Failed != 1 || res.Cells[1].Error == "" || res.Cells[1].Metrics != nil {
		t.Fatalf("cells: %+v", res.Cells)
	}
	if !strings.Contains(strings.Join(res.Summary.Limitations, " "), "failed") {
		t.Fatalf("limitations: %v", res.Summary.Limitations)
	}
}

func TestBatchRefusesMoreWorkThanItLimits(t *testing.T) {
	for _, q := range []string{
		"universes=a,b,c,d,e&periods=3",
		"periods=1,2,3,4,5,6,7",
		"universes=a,b,c,d&periods=3,5,10,2019-01-01..",
	} {
		if code, _, raw := getBatch(t, q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d %v", q, code, raw)
		}
	}
}

func TestBatchCellQuery(t *testing.T) {
	base := map[string][]string{"strategy": {"momentum"}, "top": {"5"}, "symbols": {"A,B"}, "years": {"10"}}
	q, err := batchCellQuery(base, "us_large_cap", "2020-01-01..2022-12-31")
	if err != nil || q.Get("universe") != "us_large_cap" || q.Get("from") != "2020-01-01" || q.Get("to") != "2022-12-31" || q.Get("symbols") != "" || q.Get("years") != "" || q.Get("top") != "5" {
		t.Fatalf("%v %v", q, err)
	}
	if _, err := batchCellQuery(base, "custom", "..2022-01-01"); err == nil {
		t.Error("a period without a start date is refused")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBatchReportsHowManyBacktestsHaveFinished(t *testing.T) {
	e := backtestEngine(t)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest/batch?symbols=UP,FLAT&strategy=momentum&top=1&periods=3,5&progress_id=run-abc12345", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res batchResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if _, leaked := res.Parameters["progress_id"]; leaked {
		t.Fatalf("the progress id is not a parameter of the run: %v", res.Parameters)
	}
	rec = httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest/progress?id=run-abc12345", nil))
	var p runProgress
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusOK || p.Done != 2 || p.Total != 2 || !p.Finished {
		t.Fatalf("progress %d: %+v", rec.Code, p)
	}
	rec = httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest/progress?id=unknown-run-1", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown run has no figure, not zero: %d", rec.Code)
	}
}
