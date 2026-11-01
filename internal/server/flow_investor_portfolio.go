package server

import (
	"context"
	"errors"
	"strings"

	"floweysales/internal/identity"
	"floweysales/internal/portfolio"
)

// paperPeakEquity returns the highest paper equity seen, observing the
// current one; tests replace it.
func (e *Engine) paperPeakEquity(equity float64) (float64, error) {
	if e.paperPeak != nil {
		return e.paperPeak(equity)
	}
	if e.db == nil {
		return 0, errors.New("no database")
	}
	return e.db.ObservePaperPeak(equity)
}

// portfolioCheckRows turns the analytics into rows a flow can screen: a
// summary, one row per alert and one per suggestion. peak is absent when
// unknown, so drawdown_from_peak_pct is then absent too.
func portfolioCheckRows(a portfolio.Analytics, equity float64, peak float64, hasPeak bool) []map[string]any {
	summary := map[string]any{"id": "summary", "kind": "summary", "total_value": a.TotalValue, "top_weight_pct": a.TopWeightPct,
		"effective_holdings": a.EffectiveHoldings, "equity": round2f(equity)}
	if a.VolatilityPct != nil {
		summary["volatility_pct"] = *a.VolatilityPct
	}
	if a.Beta != nil {
		summary["beta"] = *a.Beta
	}
	if a.MaxDrawdown1YPct != nil {
		summary["max_drawdown_1y_pct"] = *a.MaxDrawdown1YPct
	}
	for class, d := range a.Drift {
		summary["drift_"+class+"_pct"] = d
	}
	if hasPeak && peak > 0 && equity > 0 {
		summary["peak_equity"] = round2f(peak)
		summary["drawdown_from_peak_pct"] = round2f((equity/peak - 1) * 100)
	}
	rows := []map[string]any{summary}
	for _, alert := range a.Alerts {
		kind, detail, _ := strings.Cut(alert, ":")
		row := map[string]any{"id": "alert|" + alert, "kind": kind, "text": alert}
		if detail != "" {
			row["pair"] = detail
		}
		rows = append(rows, row)
	}
	for _, s := range a.Suggestions {
		rows = append(rows, map[string]any{"id": "suggestion|" + s.Side + "|" + s.Symbol, "kind": "suggestion", "symbol": s.Symbol,
			"side": s.Side, "amount": s.Amount, "reason": s.Reason})
	}
	return rows
}

// runPortfolioCheck measures the paper portfolio. It is admin only because
// the paper portfolio is, and it only reports.
func (e *Engine) runPortfolioCheck(ctx context.Context) (string, error) {
	admin := identity.User{Role: identity.RoleSysAdmin}
	_, summary := e.paperAccount()
	a := e.analyzePaperPortfolio(ctx, admin)
	peak, err := e.paperPeakEquity(summary.Equity)
	return flowRowsJSON(portfolioCheckRows(a, summary.Equity, peak, err == nil))
}
