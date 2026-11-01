package market

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// CorporateEvent is a dividend, split or earnings date from a provider plugin
// that declares the "events" capability. Date is "2026-11-14"; Amount is the
// dividend per share and absent when unknown, never 0; Confirmed is false for
// estimated earnings dates.
type CorporateEvent struct {
	Symbol    string   `json:"symbol"`
	Kind      string   `json:"kind"` // dividend | split | earnings
	Date      string   `json:"date"`
	Amount    *float64 `json:"amount,omitempty"`
	Currency  string   `json:"currency,omitempty"`
	Ratio     string   `json:"ratio,omitempty"`
	Confirmed bool     `json:"confirmed"`
	Provider  string   `json:"provider,omitempty"`
}

// EventSource is the optional part of a Backend that serves corporate events.
type EventSource interface {
	EventProviders() []Provider
	Events(ctx context.Context, p Provider, symbol, from, to string) ([]CorporateEvent, error)
}

// ErrNoEventProvider means no installed provider offers events.
var ErrNoEventProvider = errors.New("no installed provider plugin offers corporate events; install one with the events capability (for example Yahoo or Finnhub)")

// MaxEventWindow is the longest span one request may cover.
const MaxEventWindow = 400 * 24 * time.Hour

const eventDateLayout = "2006-01-02"

// EventWindow parses and checks an ISO date window.
func EventWindow(from, to string) (time.Time, time.Time, error) {
	f, err := time.Parse(eventDateLayout, from)
	if err != nil {
		return f, f, fmt.Errorf("from must be a date like 2026-11-01")
	}
	t, err := time.Parse(eventDateLayout, to)
	if err != nil {
		return f, t, fmt.Errorf("to must be a date like 2026-11-01")
	}
	if t.Before(f) {
		return f, t, errors.New("to must not be before from")
	}
	if t.Sub(f) > MaxEventWindow {
		return f, t, errors.New("the window may be at most 400 days")
	}
	return f, t, nil
}

// EventsFor reads events from p, or, when p is empty, from the first
// installed provider that offers them.
func EventsFor(ctx context.Context, b Backend, p Provider, symbol, from, to string) ([]CorporateEvent, error) {
	if _, _, err := EventWindow(from, to); err != nil {
		return nil, err
	}
	source, ok := b.(EventSource)
	if !ok {
		return nil, ErrNoEventProvider
	}
	if p == "" {
		providers := source.EventProviders()
		if len(providers) == 0 {
			return nil, ErrNoEventProvider
		}
		p = providers[0]
	}
	return source.Events(ctx, p, symbol, from, to)
}

// CleanEvents keeps well-formed events (a known kind, an ISO date, a finite
// non-negative amount), labels the provider and symbol and orders them by
// date. Plugin output is untrusted.
func CleanEvents(items []CorporateEvent, p Provider, symbol string) []CorporateEvent {
	out := make([]CorporateEvent, 0, len(items))
	for _, ev := range items {
		if ev.Kind != "dividend" && ev.Kind != "split" && ev.Kind != "earnings" {
			continue
		}
		if _, err := time.Parse(eventDateLayout, ev.Date); err != nil {
			continue
		}
		if ev.Amount != nil && (math.IsNaN(*ev.Amount) || math.IsInf(*ev.Amount, 0) || *ev.Amount < 0) {
			ev.Amount = nil
		}
		ev.Ratio = strings.TrimSpace(ev.Ratio)
		if ev.Provider == "" {
			ev.Provider = p.ID()
		}
		if ev.Symbol == "" {
			ev.Symbol = strings.ToUpper(strings.TrimSpace(symbol))
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}
