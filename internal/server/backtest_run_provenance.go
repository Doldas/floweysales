package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"floweysales/internal/provenance"
	"floweysales/internal/storage"
)

// backtestMethodVersion must change when the backtest rules change, so a saved
// run says which rules produced it.
const backtestMethodVersion = "3"

var backtestMethodFormula = map[string]string{
	"single":      "Rank the universe by the strategy's factors at each rebalance date using only data reported by then, hold the top names, subtract trading cost and slippage (optionally filling a number of days after the ranking), compare with the benchmark.",
	"batch":       "The single backtest repeated for each universe and period; the summary is the minimum, median and maximum over the cells that completed.",
	"montecarlo":  "The daily returns of the single backtest are resampled with a seeded generator into simulated paths; the summary is percentiles over those paths, not observed results.",
	"sensitivity": "The single backtest repeated with one setting changed at a time; the summary is the spread over the variants that completed.",
}

// backtestRunProvenance describes a saved run from what was stored with it. A
// run is calculated from provider prices and reports; the Fact names the
// method, the time the server produced the answer and, where the stored
// result records it, the provider and the last price date. A value the
// result does not record is left out and the status is "partial", never
// filled in.
func backtestRunProvenance(run storage.BacktestRun) provenance.Fact {
	formula, kindOK := backtestMethodFormula[run.Kind]
	if !kindOK {
		return provenance.Unspecified("The saved result cannot be read, so its origin is unknown.")
	}
	return resultProvenance("backtest."+run.Kind, backtestMethodVersion, formula, backtestNote, []byte(run.Result), fmt.Sprintf("backtest_run:%d", run.ID))
}

const backtestNote = "Computed from provider prices and reported quarters. Dividends are not included and the universe is today's list, so survivorship bias applies."

// resultProvenance is the origin of any calculated result whose JSON records
// "provider", "generated_at" and "data_end": backtests, strategy runs and fits.
// A value the result does not record is left out and the status is "partial".
func resultProvenance(methodID, version, formula, note string, result []byte, runID string) provenance.Fact {
	var doc struct {
		Provider    string `json:"provider"`
		GeneratedAt string `json:"generated_at"`
		DataEnd     string `json:"data_end"`
	}
	if json.Unmarshal(result, &doc) != nil {
		return provenance.Unspecified("The result cannot be read, so its origin is unknown.")
	}

	f := provenance.Fact{
		Kind:   provenance.Calculated,
		Status: provenance.Known,
		Method: &provenance.Method{ID: methodID, Version: version, Formula: formula},
		RunID:  runID,
	}
	notes := []string{note}
	if doc.Provider != "" {
		f.Provider = doc.Provider
		f.Inputs = []string{"provider:" + doc.Provider}
	} else {
		f.Status = provenance.Partial
		notes = append(notes, "The result does not record a provider.")
	}
	if t, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		t = t.UTC()
		f.RetrievedAt = &t
	} else {
		f.Status = provenance.Partial
		notes = append(notes, "The time the answer was produced is not recorded.")
	}
	if t, err := time.Parse("2006-01-02", doc.DataEnd); err == nil {
		f.AsOf = &t
	}
	f.Note = strings.Join(notes, " ")
	if err := f.Validate(); err != nil {
		return provenance.Unspecified("provenance could not be built: " + err.Error())
	}
	return f
}
