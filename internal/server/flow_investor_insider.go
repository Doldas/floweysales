package server

import (
	"context"
	"sort"
	"sync"
	"time"

	"floweysales/internal/sec"
)

// insiderClusterMin is how many different insiders must buy in the window
// for the cluster signal.
const insiderClusterMin = 3

func (e *Engine) insiderTradeList(ctx context.Context, ticker string, lookbackDays, maxFilings int) ([]sec.InsiderTrade, error) {
	if e.insiderTrades != nil {
		return e.insiderTrades(ctx, ticker, lookbackDays, maxFilings)
	}
	return sec.Production().InsiderTrades(ctx, ticker, lookbackDays, maxFilings)
}

// insiderRow summarizes the open-market purchases and sales of one company
// in the window. Figures that need a price are absent when no trade had one.
func insiderRow(symbol string, trades []sec.InsiderTrade, lookbackDays int, now time.Time) map[string]any {
	cutoff := now.AddDate(0, 0, -lookbackDays).Format("2006-01-02")
	row := map[string]any{"symbol": symbol, "lookback_days": lookbackDays}
	buyers := map[string]bool{}
	buys, sells := 0, 0
	netShares, netValue, priced := 0.0, 0.0, false
	largest, largestBuyer := 0.0, ""
	officerBuy := false
	latest, latestURL := "", ""
	for _, t := range trades {
		if t.Date < cutoff {
			continue
		}
		sign := 1.0
		if t.Code == "S" {
			sign = -1
			sells++
		} else {
			buys++
			buyers[t.Owner] = true
			officerBuy = officerBuy || t.IsOfficer
		}
		netShares += sign * t.Shares
		if t.Value != nil {
			netValue += sign * *t.Value
			priced = true
			if t.Code == "P" && *t.Value > largest {
				largest, largestBuyer = *t.Value, t.Owner
			}
		}
		if t.Date > latest {
			latest, latestURL = t.Date, t.URL
		}
	}
	row["buy_count"], row["sell_count"] = buys, sells
	row["buyers"] = len(buyers)
	row["net_shares"] = round2f(netShares)
	if priced {
		row["net_value"] = round2f(netValue)
	}
	if largest > 0 {
		row["largest_buy_value"], row["largest_buyer"] = round2f(largest), largestBuyer
	}
	row["officer_buy"] = officerBuy
	row["cluster_buy"] = len(buyers) >= insiderClusterMin
	if latest != "" {
		row["latest"], row["url"] = latest, latestURL
	}
	return row
}

// runInsiderNode reads Form 4 filings for each symbol from SEC EDGAR.
func (e *Engine) runInsiderNode(ctx context.Context, n flowNode, symbols []string, now time.Time) (string, error) {
	lookback := n.LookbackDays
	if lookback == 0 {
		lookback = 30
	}
	var mu sync.Mutex
	rows := map[string]map[string]any{}
	_, err := flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
		trades, err := e.insiderTradeList(ctx, symbol, lookback, 10)
		if err != nil {
			return nil, err
		}
		row := insiderRow(symbol, trades, lookback, now)
		mu.Lock()
		rows[symbol] = row
		mu.Unlock()
		return row, nil
	})
	if err != nil {
		return "", err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, s := range symbols {
		if row, ok := rows[s]; ok {
			out = append(out, row)
		}
	}
	// The strongest signal first: clusters, then officer buys, then value.
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i]["cluster_buy"] == true, out[j]["cluster_buy"] == true
		if ci != cj {
			return ci
		}
		vi, _ := numberOf(out[i]["largest_buy_value"])
		vj, _ := numberOf(out[j]["largest_buy_value"])
		return vi > vj
	})
	return flowRowsJSON(out)
}
