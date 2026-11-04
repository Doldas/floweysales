package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
	"floweysales/internal/storage"
)

func summaryEngine(t *testing.T, model func(call int, msgs []ai.Message) (string, error)) (*Engine, *int) {
	e := runsEngine(t)
	calls := new(int)
	e.summarize = func(_ context.Context, msgs []ai.Message) (string, error) {
		*calls++
		return model(*calls, msgs)
	}
	if rec := runCall(e, http.MethodPost, "/api/market/backtest/runs", `{"name":"Spread","kind":"batch","query":"?symbols=UP,FLAT&strategy=momentum&top=1&periods=3,5"}`); rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	return e, calls
}

type summaryBody struct {
	Verified bool
	Summary  string
	Facts    string
	Notes    []string
	Reason   string
	Attempts int
}

func summarize(t *testing.T, e *Engine) summaryBody {
	rec := runCall(e, http.MethodPost, "/api/market/backtest/runs/1/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", rec.Code, rec.Body)
	}
	var out summaryBody
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// A figure copied from the facts sheet passes.
func TestSummaryWithRecordedFiguresIsVerified(t *testing.T) {
	e, _ := summaryEngine(t, func(_ int, msgs []ai.Message) (string, error) {
		facts := msgs[1].Content
		m := regexp.MustCompile(`CAGR % (-?\d+\.\d\d)`).FindStringSubmatch(facts)
		if m == nil {
			t.Fatalf("no CAGR in facts: %s", facts)
		}
		return "Where it worked: CAGR was " + m[1] + "%.", nil
	})
	out := summarize(t, e)
	if !out.Verified || !strings.Contains(out.Summary, "CAGR was") || out.Attempts != 1 {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(out.Facts, "Saved backtest") {
		t.Errorf("facts sheet missing: %s", out.Facts)
	}
}

// An invented figure is rejected and the model gets one correction.
func TestSummaryWithInventedFigureIsRetriedOnce(t *testing.T) {
	e, calls := summaryEngine(t, func(n int, msgs []ai.Message) (string, error) {
		if n == 1 {
			return "The strategy returned 87.31% a year.", nil
		}
		last := msgs[len(msgs)-1].Content
		if !strings.Contains(last, "87.31") {
			t.Errorf("the correction must name the figure: %s", last)
		}
		return "No figure is claimed beyond the sheet.", nil
	})
	out := summarize(t, e)
	if !out.Verified || out.Attempts != 2 || *calls != 2 {
		t.Fatalf("%+v calls %d", out, *calls)
	}
}

// If the model keeps inventing, nothing unverified is shown, only the facts.
func TestSummaryThatNeverPassesIsWithheld(t *testing.T) {
	e, calls := summaryEngine(t, func(int, []ai.Message) (string, error) { return "It made 87.31% a year.", nil })
	out := summarize(t, e)
	if out.Verified || out.Summary != "" || *calls != summaryAttempts || !strings.Contains(out.Reason, "87.31%") || out.Facts == "" {
		t.Fatalf("%+v calls %d", out, *calls)
	}
}

func TestSummaryKeepsFactsWhenTheModelIsUnavailable(t *testing.T) {
	e, _ := summaryEngine(t, func(int, []ai.Message) (string, error) { return "", errors.New("connection refused") })
	out := summarize(t, e)
	if out.Verified || out.Summary != "" || !strings.Contains(out.Reason, "could not be reached") || out.Facts == "" {
		t.Fatalf("%+v", out)
	}
}

// The server, not the model, states that a sample is thin and that this is research.
func TestSummaryAlwaysCarriesTheServersNotes(t *testing.T) {
	e, _ := summaryEngine(t, func(int, []ai.Message) (string, error) { return "Nothing numeric.", nil })
	out := summarize(t, e)
	joined := strings.Join(out.Notes, " ")
	if !strings.Contains(joined, "not investment advice") || !strings.Contains(joined, "Thin sample") {
		t.Fatalf("notes %v", out.Notes)
	}
}

func TestSummaryOfAnotherUsersOrMissingRunIsNotFound(t *testing.T) {
	e, _ := summaryEngine(t, func(int, []ai.Message) (string, error) { return "x", nil })
	if rec := runCall(e, http.MethodPost, "/api/market/backtest/runs/99/summary", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing run: %d", rec.Code)
	}
	if rec := runCall(e, http.MethodPost, "/api/market/backtest/runs/abc/summary", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestOrdinaryUsersMaySummarizeTheirOwnRunOnly(t *testing.T) {
	if !allowedAPI(identity.RoleUser, "POST", "/api/market/backtest/runs/3/summary") {
		t.Error("summary must be allowed")
	}
	if allowedAPI(identity.RoleUser, "GET", "/api/market/backtest/runs/3/summary") {
		t.Error("only POST")
	}
}

func TestFactsSheetCarriesTheConsecutiveParts(t *testing.T) {
	run := storage.BacktestRun{ID: 1, Name: "n", Kind: "single", Query: "q", Result: `{"universe":"US","strategy":"value","from":"2020-01-01","to":"2023-01-01","metrics":{"cagr_pct":8.5},
		"rebalances":[{}],"segments":[{"from":"2020-01-01","to":"2021-01-01","cagr_pct":12.3,"max_drawdown_pct":-9.1,"excess_cagr_pct":2.2},{"from":"2021-01-01","to":"2022-01-01","cagr_pct":-3.4,"max_drawdown_pct":-15,"excess_cagr_pct":-5.5}],
		"segment_summary":"Beat the benchmark in 1 of 2 consecutive parts."}`}
	facts, _, err := backtestFacts(run)
	if err != nil || !strings.Contains(facts, "Part 2 of the period: 2021-01-01 to 2022-01-01; CAGR % -3.40; Excess CAGR % -5.50; Max drawdown % -15.00") ||
		!strings.Contains(facts, "Beat the benchmark in 1 of 2 consecutive parts.") {
		t.Fatalf("%v\n%s", err, facts)
	}
}
