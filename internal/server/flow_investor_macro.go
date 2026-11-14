package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/routing"
)

// Series the macro regime reads, and the row field each becomes. The ids are
// FRED's, and a provider that declares the macro capability answers them.
var macroSeries = []struct{ id, field string }{
	{"T10Y2Y", "curve_spread"}, {"UNRATE", "unemployment"}, {"FEDFUNDS", "fed_funds"}, {"BAMLH0A0HYM2", "hy_spread"}, {"VIXCLS", "vix"},
}

const macroNotInstalled = "Install or update a provider plugin that offers macro series (for example FRED 1.1.0 or later) and enter its key under Settings → Plugins."

type obs struct {
	at time.Time
	v  float64
}

func observations(s market.HistorySeries) []obs {
	out := make([]obs, 0, len(s.Points))
	for _, p := range s.Points {
		if p.Timestamp > 0 {
			out = append(out, obs{time.Unix(p.Timestamp, 0).UTC(), p.Close})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// valueOn is the last observation at or before t.
func valueOn(o []obs, t time.Time) (float64, bool) {
	for i := len(o) - 1; i >= 0; i-- {
		if !o[i].at.After(t) {
			return o[i].v, true
		}
	}
	return 0, false
}

// macroRegime reads the yield curve, unemployment, policy rate, credit
// spreads and volatility into one row and a plain regime: risk_on, caution
// or risk_off. A missing series leaves its fields and signals out and is
// listed in missing; the regime then counts only the signals it has.
func macroRegime(series map[string]market.HistorySeries, now time.Time) map[string]any {
	row := map[string]any{}
	missing := []string{}
	latestDate := time.Time{}
	data := map[string][]obs{}
	for _, m := range macroSeries {
		o := observations(series[m.id])
		if len(o) == 0 {
			missing = append(missing, m.id)
			continue
		}
		data[m.id] = o
		last := o[len(o)-1]
		row[m.field] = round2f(last.v)
		if prev, ok := valueOn(o, last.at.AddDate(0, -3, 0)); ok && o[0].at.Before(last.at.AddDate(0, -3, 0).Add(24*time.Hour)) {
			row[m.field+"_3m_change"] = round2f(last.v - prev)
		}
		if last.at.After(latestDate) {
			latestDate = last.at
		}
	}
	if !latestDate.IsZero() {
		row["as_of"] = latestDate.Format("2006-01-02")
	}
	signals, reasons := 0, []string{}
	flag := func(field string, on bool, reason string) {
		row[field] = on
		if on && reason != "" {
			signals++
			reasons = append(reasons, reason)
		}
	}

	if curve, ok := data["T10Y2Y"]; ok {
		last := curve[len(curve)-1]
		row["curve_inverted"] = last.v < 0
		uninverting := false
		for _, o := range curve {
			if o.v < 0 && o.at.After(last.at.AddDate(-1, 0, 0)) {
				uninverting = last.v >= 0
				break
			}
		}
		flag("curve_uninverting", uninverting, "yield curve is un-inverting after being inverted in the last year")
	}
	if un, ok := data["UNRATE"]; ok && len(un) >= 4 {
		avg := func(i int) float64 { return (un[i].v + un[i-1].v + un[i-2].v) / 3 }
		last := len(un) - 1
		minPrev, found := 0.0, false
		for i := 2; i < last; i++ {
			if un[i].at.Before(un[last].at.AddDate(-1, 0, 0)) {
				continue
			}
			if a := avg(i); !found || a < minPrev {
				minPrev, found = a, true
			}
		}
		if found {
			rise := avg(last) - minPrev
			row["sahm_gap"] = round2f(rise)
			flag("sahm_rule", rise >= 0.5, "unemployment rule: 3-month average is 0.5 points or more above its 12-month low")
		}
	}
	if hy, ok := data["BAMLH0A0HYM2"]; ok {
		level := hy[len(hy)-1].v
		change, _ := numberOf(row["hy_spread_3m_change"])
		flag("credit_stress", level >= 5 || change >= 1, "high-yield credit spreads are wide or rising fast")
	}
	if vix, ok := data["VIXCLS"]; ok {
		v := vix[len(vix)-1].v
		regime := "stressed"
		if v < 15 {
			regime = "calm"
		} else if v < 25 {
			regime = "normal"
		}
		row["vol_regime"] = regime
		if regime == "stressed" {
			signals++
			reasons = append(reasons, "market volatility is high (VIX 25 or more)")
		}
	}

	switch {
	case signals >= 2:
		row["regime"] = "risk_off"
	case signals == 1:
		row["regime"] = "caution"
	default:
		row["regime"] = "risk_on"
	}
	row["signals"] = signals
	row["reasons"] = strings.Join(reasons, "; ")
	if len(missing) > 0 {
		row["missing"] = strings.Join(missing, ", ")
	}
	return row
}

// servesMacro reports whether a provider's history answers the macro series.
func servesMacro(c market.ProviderCapability) bool {
	return slices.Contains(c.Capabilities, "macro")
}

// macroProvider is the provider used when no policy is saved: the first that
// serves macro series and is enabled, or else the first, so that the caller
// reports why it cannot be read (a missing key, for instance).
func (e *Engine) macroProvider() (market.Provider, bool) {
	var first market.Provider
	for _, c := range e.providers.Providers() {
		if !servesMacro(c) {
			continue
		}
		if c.Enabled {
			return market.Provider(c.ID), true
		}
		if first == "" {
			first = market.Provider(c.ID)
		}
	}
	return first, first != ""
}

// readMacroSeries reads every series from one provider. A series it cannot
// answer stays missing; reading none is an error, so routing may try the next.
func (e *Engine) readMacroSeries(ctx context.Context, provider market.Provider) (map[string]market.HistorySeries, error) {
	var mu sync.Mutex
	series := map[string]market.HistorySeries{}
	var firstErr error
	ids := make([]string, len(macroSeries))
	for i, m := range macroSeries {
		ids[i] = m.id
	}
	_, _ = flowEachSymbol(ctx, ids, func(ctx context.Context, id string) (map[string]any, error) {
		s, err := e.flowHistory(ctx, id, provider, market.Range5Y)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil, err
		}
		series[id] = s
		return map[string]any{"symbol": id}, nil
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return nil, fmt.Errorf("no macro series could be read from %s: %w", provider, firstErr)
	}
	return series, nil
}

// runMacroNode reads the macro series through the macro policy when one is
// saved. One provider answers the whole regime: series from different sources
// are never combined, so a fallback replaces the set.
func (e *Engine) runMacroNode(ctx context.Context, now time.Time) (string, error) {
	fallback, _ := e.macroProvider()
	series, outcome, routed, err := routedCall(ctx, e, 0, routing.Macro, "", fallback, e.readMacroSeries)
	var answered market.Provider
	if routed {
		answered = market.ParseProvider(outcome.Actual)
	} else {
		var ok bool
		if answered, ok = e.macroProvider(); !ok {
			return "", errors.New(macroNotInstalled)
		}
		series, err = e.readMacroSeries(ctx, answered)
	}
	if err != nil {
		if errors.Is(err, market.ErrProviderNotInstalled) {
			return "", errors.New(macroNotInstalled)
		}
		return "", err
	}
	row := macroRegime(series, now)
	row["provider"] = answered.ID()
	if routed {
		row["routing"] = outcome
	}
	return flowRowsJSON([]map[string]any{row})
}
