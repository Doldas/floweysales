package server

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/market"
)

const (
	eventsDefaultAhead = 30
	eventsMaxAhead     = 90
)

func validEventKinds(raw string) bool {
	for _, k := range splitFields(raw) {
		if k != "dividend" && k != "split" && k != "earnings" {
			return false
		}
	}
	return true
}

// eventRow is one dividend, split or earnings date. id makes Only new work
// without key fields.
func eventRow(ev market.CorporateEvent, today time.Time) map[string]any {
	row := map[string]any{"id": ev.Symbol + "|" + ev.Kind + "|" + ev.Date, "symbol": ev.Symbol, "kind": ev.Kind, "date": ev.Date,
		"confirmed": ev.Confirmed, "provider": ev.Provider}
	if d, err := time.Parse("2006-01-02", ev.Date); err == nil {
		row["days_until"] = int(d.Sub(today.Truncate(24*time.Hour)).Hours() / 24)
	}
	if ev.Amount != nil {
		row["amount"] = *ev.Amount
	}
	if ev.Currency != "" {
		row["currency"] = ev.Currency
	}
	if ev.Ratio != "" {
		row["ratio"] = ev.Ratio
	}
	return row
}

// runEventsNode lists corporate events for the symbols from the first
// installed provider that offers them. For this node limit means days ahead
// (default 30, at most 90) and lookback_days reaches into the past.
func (e *Engine) runEventsNode(ctx context.Context, n flowNode, symbols []string, now time.Time) (string, error) {
	ahead := n.Limit
	if ahead == 0 {
		ahead = eventsDefaultAhead
	}
	from := now.AddDate(0, 0, -n.LookbackDays).Format("2006-01-02")
	to := now.AddDate(0, 0, ahead).Format("2006-01-02")
	kinds := map[string]bool{}
	for _, k := range splitFields(n.Kinds) {
		kinds[k] = true
	}
	var provider market.Provider
	if n.Provider != "" {
		provider = market.ParseProvider(n.Provider)
	}
	var mu sync.Mutex
	rows := []map[string]any{}
	_, err := flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
		events, err := market.EventsFor(ctx, e.providers, provider, symbol, from, to)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		for _, ev := range events {
			if len(kinds) == 0 || kinds[ev.Kind] {
				rows = append(rows, eventRow(ev, now))
			}
		}
		return map[string]any{"symbol": symbol}, nil
	})
	if err != nil {
		return "", err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if a, b := rows[i]["date"].(string), rows[j]["date"].(string); a != b {
			return a < b
		}
		return strings.Compare(rows[i]["symbol"].(string), rows[j]["symbol"].(string)) < 0
	})
	return flowRowsJSON(rows)
}
