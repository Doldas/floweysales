package server

import (
	"encoding/json"
	"fmt"
	"time"

	"floweysales/internal/provenance"
	"floweysales/internal/storage"
)

// backtestMethodVersion must change when the backtest rules change, so a saved
// run says which rules produced it.
const backtestMethodVersion = "1"

var backtestMethodFormula = map[string]string{
	"single":      "Rank the universe by the strategy's factors at each rebalance date using only data reported by then, hold the top names, subtract trading cost, compare with the benchmark.",
	"batch":       "The single backtest repeated for each universe and period; the summary is the minimum, median and maximum over the cells that completed.",
	"sensitivity": "The single backtest repeated with one setting changed at a time; the summary is the spread over the variants that completed.",
}

// backtestRunProvenance describes a saved run from what was stored with it. A
// run is calculated from provider prices and reports; the Fact names the
// method, the time the server produced the answer and, where the stored
// result records it, the provider and the last price date. A value the
// result does not record is left out and the status is "partial", never
// filled in.
func backtestRunProvenance(run storage.BacktestRun) provenance.Fact {
	var doc struct {
		Provider    string `json:"provider"`
		GeneratedAt string `json:"generated_at"`
		DataEnd     string `json:"data_end"`
	}
	_ = json.Unmarshal([]byte(run.Result), &doc)

	f := provenance.Fact{
		Kind:   provenance.Calculated,
		Status: provenance.Known,
		Method: &provenance.Method{ID: "backtest." + run.Kind, Version: backtestMethodVersion, Formula: backtestMethodFormula[run.Kind]},
		RunID:  fmt.Sprintf("backtest_run:%d", run.ID),
		Note:   "Computed from provider prices and reported quarters as of the time shown. Dividends are not included and the universe is today's list, so survivorship bias applies.",
	}
	if doc.Provider != "" {
		f.Provider = doc.Provider
		f.Inputs = []string{"provider:" + doc.Provider}
	} else {
		f.Status = provenance.Partial
		f.Note += " The provider is not recorded for this kind of run."
	}
	if t, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		t = t.UTC()
		f.RetrievedAt = &t
	}
	if t, err := time.Parse("2006-01-02", doc.DataEnd); err == nil {
		f.AsOf = &t
	}
	if err := f.Validate(); err != nil {
		return provenance.Unspecified("provenance could not be built: " + err.Error())
	}
	return f
}
