package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/backtest"
)

// A sensitivity run changes one setting at a time around a base backtest: the
// trading cost, the number of holdings and the rebalance interval. It shows how
// much the result leans on a setting, which a single run cannot.

const (
	sensitivityMaxValues = 6
	sensitivityTimeout   = 6 * time.Minute
)

type sensitivityVariant struct {
	Parameter string            `json:"parameter"` // cost_bps | top | rebalance
	Value     string            `json:"value"`
	Base      bool              `json:"base,omitempty"`
	Metrics   *backtest.Metrics `json:"metrics,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type sensitivitySummary struct {
	Completed     int          `json:"completed"`
	Failed        int          `json:"failed"`
	CAGRPct       *batchSpread `json:"cagr_pct,omitempty"`
	ExcessCAGRPct *batchSpread `json:"excess_cagr_pct,omitempty"`
	// PositiveExcess counts variants with a positive excess return, of those that had a benchmark.
	PositiveExcess int `json:"positive_excess"`
	WithBenchmark  int `json:"with_benchmark"`
	// CostBreakEvenBps is the lowest tested cost at which the excess return is zero or negative; absent when none was.
	CostBreakEvenBps *float64 `json:"cost_break_even_bps,omitempty"`
	Notes            []string `json:"notes"`
}

type sensitivityResponse struct {
	Universe    string               `json:"universe"`
	Strategy    string               `json:"strategy"`
	Period      string               `json:"period"`
	GeneratedAt string               `json:"generated_at"`
	Base        map[string]string    `json:"base"`
	Variants    []sensitivityVariant `json:"variants"`
	Summary     sensitivitySummary   `json:"summary"`
}

func floatList(raw string, def []float64) ([]float64, error) {
	parts := splitList(raw)
	if len(parts) == 0 {
		return def, nil
	}
	if len(parts) > sensitivityMaxValues {
		return nil, fmt.Errorf("give at most %d values per setting", sensitivityMaxValues)
	}
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", p)
		}
		out = append(out, v)
	}
	return out, nil
}

func (e *Engine) runSensitivity(ctx context.Context, q url.Values) (sensitivityResponse, error) {
	baseReq, err := e.parseBacktestQuery(q)
	if err != nil {
		return sensitivityResponse{}, err
	}
	costs, err := floatList(q.Get("costs"), []float64{0, 10, 25, 50})
	if err != nil {
		return sensitivityResponse{}, badRequest{fmt.Errorf("costs: %w", err)}
	}
	topsF, err := floatList(q.Get("tops"), []float64{5, 10, 15})
	if err != nil {
		return sensitivityResponse{}, badRequest{fmt.Errorf("tops: %w", err)}
	}
	period := "years=" + strconv.Itoa(baseReq.years)
	if baseReq.years == 0 {
		period = baseReq.from.Format(strategyDateLayout) + ".." + baseReq.to.Format(strategyDateLayout)
	}
	out := sensitivityResponse{Universe: baseReq.universe, Strategy: baseReq.strategy.ID, Period: period, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Base: map[string]string{"cost_bps": fmt.Sprint(baseReq.costBps), "top": strconv.Itoa(baseReq.top), "rebalance": baseReq.rebalLabel}}

	type variant struct {
		parameter, value string
		base             bool
		query            url.Values
	}
	with := func(k, v string) url.Values {
		c := url.Values{}
		for key, val := range q {
			c[key] = val
		}
		c.Set(k, v)
		return c
	}
	variants := []variant{{"base", "", true, q}}
	seen := map[string]bool{}
	add := func(parameter, value string, query url.Values) {
		if key := parameter + "=" + value; !seen[key] {
			seen[key] = true
			variants = append(variants, variant{parameter: parameter, value: value, query: query})
		}
	}
	for _, c := range costs {
		if c != baseReq.costBps {
			add("cost_bps", fmt.Sprint(c), with("cost_bps", fmt.Sprint(c)))
		}
	}
	skipped := 0
	for _, t := range topsF {
		n := int(t)
		switch {
		case n == baseReq.top:
		case n < 1 || n > len(baseReq.symbols):
			skipped++
		default:
			add("top", strconv.Itoa(n), with("top", strconv.Itoa(n)))
		}
	}
	other := "quarterly"
	if baseReq.rebalLabel == "quarterly" {
		other = "monthly"
	}
	add("rebalance", other, with("rebalance", other))

	ctx, cancel := context.WithTimeout(ctx, sensitivityTimeout)
	defer cancel()
	var cagr, excess []float64
	sum := &out.Summary
	for _, v := range variants {
		item := sensitivityVariant{Parameter: v.parameter, Value: v.value, Base: v.base}
		if v.base {
			item.Parameter, item.Value = "base", "as asked"
		}
		req, err := e.parseBacktestQuery(v.query)
		var res backtestResponse
		if err == nil {
			res, err = e.backtestCached(ctx, req)
		}
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				err = errors.New("the run ran out of time before this variant")
			}
			item.Error = err.Error()
			sum.Failed++
		} else {
			m := res.Metrics
			item.Metrics = &m
			cagr = append(cagr, m.CAGRPct)
			if m.ExcessCAGRPct != nil {
				sum.WithBenchmark++
				excess = append(excess, *m.ExcessCAGRPct)
				if *m.ExcessCAGRPct > 0 {
					sum.PositiveExcess++
				} else if v.parameter == "cost_bps" {
					c, _ := strconv.ParseFloat(v.value, 64)
					if sum.CostBreakEvenBps == nil || c < *sum.CostBreakEvenBps {
						sum.CostBreakEvenBps = &c
					}
				}
			}
		}
		out.Variants = append(out.Variants, item)
		if errors.Is(ctx.Err(), context.Canceled) {
			return out, ctx.Err()
		}
	}
	sum.Completed = len(out.Variants) - sum.Failed
	sum.CAGRPct, sum.ExcessCAGRPct = spreadOf(cagr), spreadOf(excess)
	sum.Notes = sensitivityNotes(out, skipped)
	return out, nil
}

// sensitivityNotes state what the variants do and do not show.
func sensitivityNotes(r sensitivityResponse, skippedTops int) []string {
	notes := []string{
		"One setting changes at a time; combinations of changes were not tested.",
		"Every variant replays the same period and the same universe, so they share the same survivorship bias and the same market path.",
	}
	if skippedTops > 0 {
		notes = append(notes, fmt.Sprintf("%d holdings counts were skipped because the universe has too few symbols.", skippedTops))
	}
	if r.Summary.Failed > 0 {
		notes = append(notes, fmt.Sprintf("%d variants failed and are not in the spread; see each variant's error.", r.Summary.Failed))
	}
	if s := r.Summary.CAGRPct; s != nil && !math.IsNaN(s.Max-s.Min) && s.Max-s.Min >= 5 {
		notes = append(notes, fmt.Sprintf("Return per year ranges over %.1f points across the variants, so the result depends noticeably on these settings.", s.Max-s.Min))
	}
	return notes
}

// registerBacktestSensitivityRoutes serves GET /api/market/backtest/sensitivity.
func (e *Engine) registerBacktestSensitivityRoutes() {
	e.mux.HandleFunc("GET /api/market/backtest/sensitivity", func(w http.ResponseWriter, r *http.Request) {
		res, err := e.runSensitivity(r.Context(), r.URL.Query())
		if err != nil {
			status := http.StatusInternalServerError
			var bad badRequest
			switch {
			case errors.As(err, &bad):
				status = http.StatusBadRequest
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]any{"error": strings.TrimSpace(err.Error())})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}
