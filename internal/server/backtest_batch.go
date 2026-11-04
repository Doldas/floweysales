package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"floweysales/internal/backtest"
)

// A batch runs one strategy over several universes and periods, so a result
// that depends on one market or one window shows up as spread. Each cell is an
// ordinary backtest (cached like one); the batch adds a summary of the spread.

const (
	batchMaxUniverses = 4
	batchMaxPeriods   = 6
	batchMaxCells     = 12
	batchTimeout      = 6 * time.Minute
)

type batchCell struct {
	Universe  string            `json:"universe"`
	Period    string            `json:"period"`
	From      string            `json:"from,omitempty"`
	To        string            `json:"to,omitempty"`
	DataStart string            `json:"data_start,omitempty"`
	DataEnd   string            `json:"data_end,omitempty"`
	Notice    string            `json:"notice,omitempty"`
	Rebalance int               `json:"rebalances,omitempty"`
	Metrics   *backtest.Metrics `json:"metrics,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// batchSpread is a minimum, median and maximum of one figure over the cells that have it.
type batchSpread struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Max    float64 `json:"max"`
}

type batchSummary struct {
	Cells          int          `json:"cells"`
	Completed      int          `json:"completed"`
	Failed         int          `json:"failed"`
	CAGRPct        *batchSpread `json:"cagr_pct,omitempty"`
	ExcessCAGRPct  *batchSpread `json:"excess_cagr_pct,omitempty"`
	MaxDrawdownPct *batchSpread `json:"max_drawdown_pct,omitempty"`
	Sharpe         *batchSpread `json:"sharpe,omitempty"`
	// BeatBenchmark counts cells with a positive excess return, out of those that had a benchmark.
	BeatBenchmark  int      `json:"beat_benchmark"`
	WithBenchmark  int      `json:"with_benchmark"`
	NegativeReturn int      `json:"negative_return"`
	Weakest        string   `json:"weakest,omitempty"`
	Strongest      string   `json:"strongest,omitempty"`
	Limitations    []string `json:"limitations"`
}

type batchResponse struct {
	Strategy    string       `json:"strategy"`
	GeneratedAt string       `json:"generated_at"`
	Universes   []string     `json:"universes"`
	Periods     []string     `json:"periods"`
	Parameters  url.Values   `json:"parameters"`
	Cells       []batchCell  `json:"cells"`
	Summary     batchSummary `json:"summary"`
}

func spreadOf(values []float64) *batchSpread {
	if len(values) == 0 {
		return nil
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	mid := v[len(v)/2]
	if len(v)%2 == 0 {
		mid = (v[len(v)/2-1] + v[len(v)/2]) / 2
	}
	r := func(x float64) float64 { return math.Round(x*100) / 100 }
	return &batchSpread{Min: r(v[0]), Median: r(mid), Max: r(v[len(v)-1])}
}

// splitList splits a comma list, dropping blanks and duplicates, keeping order.
func splitList(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

// batchCellQuery makes the single-backtest query for one universe and period.
// A period is 3, 5 or 10 (years), or "2019-01-01..2022-12-31".
func batchCellQuery(base url.Values, universe, period string) (url.Values, error) {
	q := url.Values{}
	for k, v := range base {
		switch k {
		case "universes", "periods", "universe", "years", "from", "to", "progress_id":
		case "symbols":
			if universe == "custom" {
				q[k] = v
			}
		default:
			q[k] = v
		}
	}
	q.Set("universe", universe)
	if from, to, ok := strings.Cut(period, ".."); ok {
		if from == "" {
			return nil, fmt.Errorf("period %q needs a start date before ..", period)
		}
		q.Set("from", from)
		if to != "" {
			q.Set("to", to)
		}
		return q, nil
	}
	q.Set("years", period)
	return q, nil
}

func (e *Engine) runBacktestBatch(ctx context.Context, base url.Values) (batchResponse, error) {
	universes := splitList(base.Get("universes"))
	if len(universes) == 0 && strings.TrimSpace(base.Get("symbols")) != "" {
		universes = []string{"custom"}
	} else if len(universes) == 0 {
		universes = []string{leaderboardUniverses[0].ID}
	}
	periods := splitList(base.Get("periods"))
	if len(periods) == 0 {
		periods = []string{"3", "5", "10"}
	}
	switch {
	case len(universes) > batchMaxUniverses:
		return batchResponse{}, badRequest{fmt.Errorf("a batch takes at most %d universes", batchMaxUniverses)}
	case len(periods) > batchMaxPeriods:
		return batchResponse{}, badRequest{fmt.Errorf("a batch takes at most %d periods", batchMaxPeriods)}
	case len(universes)*len(periods) > batchMaxCells:
		return batchResponse{}, badRequest{fmt.Errorf("a batch runs at most %d backtests; %d universes x %d periods is %d", batchMaxCells, len(universes), len(periods), len(universes)*len(periods))}
	}
	out := batchResponse{GeneratedAt: time.Now().UTC().Format(time.RFC3339), Universes: universes, Periods: periods, Parameters: url.Values{}}
	for k, v := range base {
		if k != "universes" && k != "periods" && k != "progress_id" {
			out.Parameters[k] = v
		}
	}
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()

	progressID, total := base.Get("progress_id"), len(universes)*len(periods)
	e.runProgress.set(progressID, "Backtesting", 0, total, false)
	defer func() {
		p, _ := e.runProgress.get(progressID)
		e.runProgress.set(progressID, "Backtesting", p.Done, total, true)
	}()

	var cagr, excess, drawdown, sharpe []float64
	sum := &out.Summary
	bestCAGR, worstCAGR := math.Inf(-1), math.Inf(1)
	for _, universe := range universes {
		for _, period := range periods {
			cell := batchCell{Universe: universe, Period: period}
			q, err := batchCellQuery(base, universe, period)
			var req backtestRequest
			if err == nil {
				req, err = e.parseBacktestQuery(q)
			}
			if err == nil {
				if out.Strategy == "" {
					out.Strategy = req.strategy.ID
				}
				var res backtestResponse
				if res, err = e.backtestCached(ctx, req); err == nil {
					m := res.Metrics
					cell.From, cell.To, cell.DataStart, cell.DataEnd, cell.Notice = res.From, res.To, res.DataStart, res.DataEnd, res.Notice
					cell.Rebalance, cell.Metrics, cell.Warnings = len(res.Rebalances), &m, res.Warnings
					label := universe + " / " + period
					cagr = append(cagr, m.CAGRPct)
					drawdown = append(drawdown, m.MaxDrawdownPct)
					sharpe = append(sharpe, m.SharpeRatio)
					if m.CAGRPct < 0 {
						sum.NegativeReturn++
					}
					if m.CAGRPct > bestCAGR {
						bestCAGR, sum.Strongest = m.CAGRPct, label
					}
					if m.CAGRPct < worstCAGR {
						worstCAGR, sum.Weakest = m.CAGRPct, label
					}
					if m.ExcessCAGRPct != nil {
						sum.WithBenchmark++
						excess = append(excess, *m.ExcessCAGRPct)
						if *m.ExcessCAGRPct > 0 {
							sum.BeatBenchmark++
						}
					}
				}
			}
			if err != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					err = errors.New("the batch ran out of time before this backtest")
				}
				cell.Error = err.Error()
				sum.Failed++
			}
			out.Cells = append(out.Cells, cell)
			e.runProgress.set(progressID, "Backtesting", len(out.Cells), total, false)
			if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
				return out, ctx.Err()
			}
		}
	}
	sum.Cells, sum.Completed = len(out.Cells), len(out.Cells)-sum.Failed
	sum.CAGRPct, sum.ExcessCAGRPct = spreadOf(cagr), spreadOf(excess)
	sum.MaxDrawdownPct, sum.Sharpe = spreadOf(drawdown), spreadOf(sharpe)
	sum.Limitations = batchLimitations(out)
	return out, nil
}

// batchLimitations states what the spread cannot show. They are facts about
// how the cells were built, not judgments of the strategy.
func batchLimitations(b batchResponse) []string {
	notes := []string{
		"Periods that end today overlap, so the cells are not independent samples; three agreeing windows are not three confirmations.",
		"Every universe is today's list of companies, so each cell carries survivorship bias.",
	}
	if b.Summary.Completed < 6 {
		notes = append(notes, fmt.Sprintf("Only %d backtests completed; the spread is too small to say how the strategy behaves across conditions.", b.Summary.Completed))
	}
	if b.Summary.Failed > 0 {
		notes = append(notes, fmt.Sprintf("%d backtests failed and are not in the spread; see each cell's error.", b.Summary.Failed))
	}
	return notes
}

// registerBacktestBatchRoutes serves GET /api/market/backtest/batch.
func (e *Engine) registerBacktestBatchRoutes() {
	e.mux.HandleFunc("GET /api/market/backtest/batch", func(w http.ResponseWriter, r *http.Request) {
		res, err := e.runBacktestBatch(r.Context(), r.URL.Query())
		if err != nil {
			status := http.StatusInternalServerError
			var bad badRequest
			switch {
			case errors.As(err, &bad):
				status = http.StatusBadRequest
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}
