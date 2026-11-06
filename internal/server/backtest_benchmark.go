package server

import (
	"sort"
	"strings"

	"floweysales/internal/market"
)

// benchmarkInfo says what a backtest was compared with, in terms an investor can check: which symbol, why
// it was chosen, what the price series includes, the currency it is quoted in and the dates it covers.
// Anything the provider did not state is left empty, not guessed.
type benchmarkInfo struct {
	Symbol string `json:"symbol"`
	// Source is "universe_default" when the list's own index was used and "chosen" when the caller picked one.
	Source string `json:"source"`
	Basis  string `json:"basis"`
	// Currency is the quote currency the provider reported; empty when it did not say.
	Currency string `json:"currency,omitempty"`
	FirstBar string `json:"first_bar,omitempty"`
	LastBar  string `json:"last_bar,omitempty"`
	// Comparable is false when the currency is known to differ from the symbols' or when it cannot be checked.
	Comparable bool   `json:"currency_comparable"`
	Note       string `json:"note,omitempty"`
}

// currencyAndBenchmark lists the quote currencies of the universe and describes the benchmark. mixed is
// true when the universe itself has more than one known currency; differs when the benchmark's known
// currency is not among the universe's known currencies.
func currencyAndBenchmark(series map[string]market.HistorySeries, bench *market.HistorySeries, benchSymbol, universeID string) (currencies []string, info *benchmarkInfo, mixed, differs bool) {
	set := map[string]bool{}
	for _, s := range series {
		if c := strings.ToUpper(strings.TrimSpace(s.Currency)); c != "" {
			set[c] = true
		}
	}
	for c := range set {
		currencies = append(currencies, c)
	}
	sort.Strings(currencies)
	mixed = len(currencies) > 1
	if bench == nil {
		return currencies, nil, mixed, false
	}
	info = &benchmarkInfo{Symbol: benchSymbol, Source: "chosen",
		Basis: "Closing prices as the provider returned them; dividends are not added, so the benchmark's total return is understated."}
	defaultBench := backtestDefaultBenchmark
	if u, ok := findLeaderboardUniverse(universeID); ok && u.Benchmark != "" {
		defaultBench = u.Benchmark
	}
	if strings.EqualFold(benchSymbol, defaultBench) {
		info.Source = "universe_default"
	}
	info.Currency = strings.ToUpper(strings.TrimSpace(bench.Currency))
	info.FirstBar, info.LastBar = barDates(bench.Points)
	switch {
	case info.Currency == "" || len(currencies) == 0:
		info.Note = "The provider did not state a currency for the benchmark or the symbols, so whether they are comparable could not be checked."
	case !set[info.Currency]:
		differs = true
		info.Note = "The benchmark is quoted in " + info.Currency + " while the symbols are quoted in " + strings.Join(currencies, ", ") + "; exchange-rate moves are not removed."
	case mixed:
		info.Note = "The symbols are quoted in several currencies, so no single benchmark is like for like for all of them."
	default:
		info.Comparable = true
	}
	return currencies, info, mixed, differs
}
