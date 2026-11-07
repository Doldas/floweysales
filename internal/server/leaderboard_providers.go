package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"floweysales/internal/market"
)

// withFallbackProvider fills the symbols the primary provider could not load
// from a second provider and, with crossCheck, compares the last prices of
// every symbol both have. An empty name or the primary itself changes nothing.
func (e *Engine) withFallbackProvider(ctx context.Context, name string, crossCheck bool, primary market.Provider, rows []map[string]any) ([]map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return rows, nil
	}
	fb := e.parseProvider(name)
	if fb.String() == primary.String() {
		return rows, nil
	}
	var wanted []string
	if crossCheck {
		for _, row := range rows {
			wanted = append(wanted, fmt.Sprint(row["symbol"]))
		}
	} else {
		wanted = failedSymbols(rows)
	}
	if len(wanted) == 0 {
		return mergeProviderRows(primary.String(), rows, fb.String(), nil), nil
	}
	other, err := e.leaderboardMetrics(ctx, fb, wanted)
	if err != nil {
		return nil, err
	}
	return mergeProviderRows(primary.String(), rows, fb.String(), other), nil
}

func removeSymbol(symbols []string, symbol string) []string {
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		if s != symbol {
			out = append(out, s)
		}
	}
	return out
}

// providersUsed counts the ranked rows each provider supplied, so a ranking
// built from two providers says so.
func providersUsed(primary string, rows []map[string]any) []map[string]any {
	counts := map[string]int{}
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			continue
		}
		p, _ := row["data_provider"].(string)
		if p == "" {
			p = primary
		}
		counts[p]++
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []map[string]any{}
	for _, n := range names {
		out = append(out, map[string]any{"provider": n, "companies": counts[n], "primary": n == primary})
	}
	return out
}

// identifierOf is what is recorded about a security's identity. A missing
// ISIN is stated, never filled in.
func identifierOf(universe leaderboardUniverse, symbol string) map[string]any {
	id := universe.Identifiers[symbol]
	out := map[string]any{"symbol": symbol, "isin": nil, "isin_status": "not recorded", "name": nil}
	if id.ISIN != "" {
		out["isin"], out["isin_status"] = id.ISIN, "recorded"
	}
	if id.Name != "" {
		out["name"] = id.Name
	}
	return out
}
