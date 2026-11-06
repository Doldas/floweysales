package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/provenance"
	"floweysales/internal/storage"
)

// A saved backtest keeps the query that was asked and the answer the server
// gave, so a result can be reopened, compared and exported later with the
// parameters and assumptions it was made with. The result is produced by the
// server when the run is saved (from the cache when it is fresh), never taken
// from the client.

type backtestRunStore interface {
	ListBacktestRuns(ctx context.Context, userID int64) ([]storage.BacktestRun, error)
	GetBacktestRun(ctx context.Context, userID, id int64) (storage.BacktestRun, error)
	SaveBacktestRun(ctx context.Context, userID int64, r storage.BacktestRun) (int64, error)
	DeleteBacktestRun(ctx context.Context, userID, id int64) error
}

func (e *Engine) backtestRunStore() backtestRunStore {
	if e.backtestRuns != nil {
		return e.backtestRuns
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

const backtestRunQueryMax = 1000

type saveBacktestRunBody struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Query string `json:"query"`
}

// computeBacktestRun runs the query for its kind and returns the response.
func (e *Engine) computeBacktestRun(ctx context.Context, kind string, q url.Values) (any, error) {
	switch kind {
	case "single":
		req, err := e.parseBacktestQuery(ctx, q)
		if err != nil {
			return nil, err
		}
		return e.backtestCached(ctx, req)
	case "batch":
		return e.runBacktestBatch(ctx, q)
	case "sensitivity":
		return e.runSensitivity(ctx, q)
	case "montecarlo":
		return e.runMonteCarlo(ctx, q)
	}
	return nil, badRequest{errors.New("kind must be single, batch, sensitivity or montecarlo")}
}

// savedBacktest is what a saved run returns: the run, its query and the result.
type savedBacktest struct {
	storage.BacktestRun
	Result     json.RawMessage `json:"result"`
	Provenance provenance.Fact `json:"provenance"`
}

func (e *Engine) registerBacktestRunRoutes() {
	runUser := func(w http.ResponseWriter, r *http.Request) (int64, backtestRunStore, bool) {
		u, ok := e.planUser(w, r)
		if !ok {
			return 0, nil, false
		}
		store := e.backtestRunStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Saved backtests are unavailable"})
			return 0, nil, false
		}
		return u.ID, store, true
	}
	runID := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid saved backtest id"})
			return 0, false
		}
		return id, true
	}

	e.mux.HandleFunc("GET /api/market/backtest/runs", func(w http.ResponseWriter, r *http.Request) {
		uid, store, ok := runUser(w, r)
		if !ok {
			return
		}
		list, err := store.ListBacktestRuns(r.Context(), uid)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": "Saved backtests are unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": list, "max": storage.MaxBacktestRuns})
	})

	e.mux.HandleFunc("POST /api/market/backtest/runs", func(w http.ResponseWriter, r *http.Request) {
		uid, store, ok := runUser(w, r)
		if !ok {
			return
		}
		var body saveBacktestRunBody
		if !decodeBody(w, r, 4<<10, &body) {
			return
		}
		body.Name, body.Query = strings.TrimSpace(body.Name), strings.TrimPrefix(strings.TrimSpace(body.Query), "?")
		if body.Name == "" || len(body.Name) > 80 || len(body.Query) > backtestRunQueryMax {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Give a name of 1 to 80 characters and a query under 1000 characters."})
			return
		}
		q, err := url.ParseQuery(body.Query)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The query is not valid."})
			return
		}
		res, err := e.computeBacktestRun(r.Context(), body.Kind, q)
		if err != nil {
			status := providerErrorStatus(err)
			var bad badRequest
			if errors.As(err, &bad) {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		raw, err := json.Marshal(res)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not store this result"})
			return
		}
		id, err := store.SaveBacktestRun(r.Context(), uid, storage.BacktestRun{Name: body.Name, Kind: body.Kind, Query: body.Query, Result: string(raw)})
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	})

	e.mux.HandleFunc("GET /api/market/backtest/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		uid, store, ok := runUser(w, r)
		id, idOK := runID(w, r)
		if !ok || !idOK {
			return
		}
		run, err := store.GetBacktestRun(r.Context(), uid, id)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, savedBacktest{BacktestRun: run, Result: json.RawMessage(run.Result), Provenance: backtestRunProvenance(run)})
	})

	e.mux.HandleFunc("DELETE /api/market/backtest/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		uid, store, ok := runUser(w, r)
		id, idOK := runID(w, r)
		if !ok || !idOK {
			return
		}
		if err := store.DeleteBacktestRun(r.Context(), uid, id); err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})

	e.mux.HandleFunc("GET /api/market/backtest/runs/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		uid, store, ok := runUser(w, r)
		id, idOK := runID(w, r)
		if !ok || !idOK {
			return
		}
		run, err := store.GetBacktestRun(r.Context(), uid, id)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		name := fmt.Sprintf("backtest-%d", run.ID)
		if r.URL.Query().Get("format") == "csv" {
			rows, err := backtestRunCSV(run)
			if err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
				return
			}
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
			cw := csv.NewWriter(w)
			_ = cw.WriteAll(rows)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.json"`)
		writeJSON(w, http.StatusOK, map[string]any{
			"name": run.Name, "kind": run.Kind, "query": run.Query, "saved_at": run.CreatedAt.UTC().Format(time.RFC3339),
			"note":       "The result is the server's answer when the run was saved. Dates are UTC. Re-running the query later may give a different result as prices and reports change.",
			"provenance": backtestRunProvenance(run),
			"result":     json.RawMessage(run.Result),
		})
	})
}

var backtestCSVMetrics = []string{"cagr_pct", "excess_cagr_pct", "max_drawdown_pct", "sharpe", "volatility_pct", "hit_rate_pct", "avg_turnover_pct"}

// backtestRunCSV flattens a saved run to a table: one row per cell or variant,
// one row for a single backtest. Missing figures are empty, never zero.
func backtestRunCSV(run storage.BacktestRun) ([][]string, error) {
	if run.Kind == "montecarlo" {
		return montecarloCSV(run)
	}
	type metricsMap = map[string]*float64
	cellMetrics := func(raw json.RawMessage) []string {
		var m metricsMap
		_ = json.Unmarshal(raw, &m)
		out := make([]string, len(backtestCSVMetrics))
		for i, k := range backtestCSVMetrics {
			if v := m[k]; v != nil {
				out[i] = strconv.FormatFloat(*v, 'f', 2, 64)
			}
		}
		return out
	}
	header := func(lead ...string) []string {
		return append(append([]string{}, lead...), append(backtestCSVMetrics, "error")...)
	}
	var doc struct {
		Metrics  json.RawMessage `json:"metrics"`
		Universe string          `json:"universe"`
		Strategy string          `json:"strategy"`
		From     string          `json:"from"`
		To       string          `json:"to"`
		Trades   *int            `json:"trades"`
		Stress   []struct {
			Name           string   `json:"name"`
			From           string   `json:"from"`
			To             string   `json:"to"`
			Days           int      `json:"days"`
			ReturnPct      float64  `json:"return_pct"`
			MaxDrawdownPct float64  `json:"max_drawdown_pct"`
			BenchReturnPct *float64 `json:"benchmark_return_pct"`
			ExcessPct      *float64 `json:"excess_pct"`
		} `json:"stress"`
		Regimes []struct {
			Name         string   `json:"name"`
			Days         int      `json:"days"`
			SharePct     float64  `json:"share_pct"`
			CAGRPct      *float64 `json:"cagr_pct"`
			BenchCAGRPct *float64 `json:"benchmark_cagr_pct"`
			ExcessPct    *float64 `json:"excess_cagr_pct"`
		} `json:"regimes"`
		Cells []struct {
			Universe string          `json:"universe"`
			Period   string          `json:"period"`
			Metrics  json.RawMessage `json:"metrics"`
			Error    string          `json:"error"`
		} `json:"cells"`
		Variants []struct {
			Parameter string          `json:"parameter"`
			Value     string          `json:"value"`
			Metrics   json.RawMessage `json:"metrics"`
			Error     string          `json:"error"`
		} `json:"variants"`
	}
	if err := json.Unmarshal([]byte(run.Result), &doc); err != nil {
		return nil, errors.New("the saved result cannot be read")
	}
	switch run.Kind {
	case "single":
		rows := [][]string{header("universe", "strategy", "from", "to"), append([]string{doc.Universe, doc.Strategy, doc.From, doc.To}, append(cellMetrics(doc.Metrics), "")...)}
		// Stress windows and regimes follow as their own tables; a missing figure is empty, never zero.
		opt := func(v *float64) string {
			if v == nil {
				return ""
			}
			return strconv.FormatFloat(*v, 'f', 2, 64)
		}
		if doc.Trades != nil {
			rows = append(rows, []string{}, []string{"trades", strconv.Itoa(*doc.Trades)})
		}
		if len(doc.Stress) > 0 {
			rows = append(rows, []string{}, []string{"stress_window", "from", "to", "days", "return_pct", "max_drawdown_pct", "benchmark_return_pct", "excess_pct"})
			for _, st := range doc.Stress {
				rows = append(rows, []string{st.Name, st.From, st.To, strconv.Itoa(st.Days), strconv.FormatFloat(st.ReturnPct, 'f', 2, 64),
					strconv.FormatFloat(st.MaxDrawdownPct, 'f', 2, 64), opt(st.BenchReturnPct), opt(st.ExcessPct)})
			}
		}
		if len(doc.Regimes) > 0 {
			rows = append(rows, []string{}, []string{"regime", "days", "share_pct", "cagr_pct", "benchmark_cagr_pct", "excess_cagr_pct"})
			for _, rg := range doc.Regimes {
				rows = append(rows, []string{rg.Name, strconv.Itoa(rg.Days), strconv.FormatFloat(rg.SharePct, 'f', 2, 64), opt(rg.CAGRPct), opt(rg.BenchCAGRPct), opt(rg.ExcessPct)})
			}
		}
		return rows, nil
	case "batch":
		rows := [][]string{header("universe", "period")}
		for _, c := range doc.Cells {
			rows = append(rows, append([]string{c.Universe, c.Period}, append(cellMetrics(c.Metrics), c.Error)...))
		}
		return rows, nil
	case "sensitivity":
		rows := [][]string{header("setting", "value")}
		for _, v := range doc.Variants {
			rows = append(rows, append([]string{v.Parameter, v.Value}, append(cellMetrics(v.Metrics), v.Error)...))
		}
		return rows, nil
	}
	return nil, errors.New("unknown kind")
}
