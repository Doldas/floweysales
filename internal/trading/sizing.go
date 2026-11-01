package trading

import (
	"errors"
	"fmt"
	"math"
)

// SizeRequest asks how many units to trade so that being stopped out loses
// RiskPct of equity. Give a Stop, or an ATR and multiple to derive one.
type SizeRequest struct {
	Equity    float64 `json:"equity"`
	RiskPct   float64 `json:"risk_pct"` // 0.1–5, default 1
	Entry     float64 `json:"entry"`
	Stop      float64 `json:"stop,omitempty"`
	ATR       float64 `json:"atr,omitempty"`
	ATRMult   float64 `json:"atr_mult,omitempty"`       // default 2
	MaxWeight float64 `json:"max_weight_pct,omitempty"` // default 20
	Side      string  `json:"side"`                     // buy | sell (short)
	// Fractional allows 8 decimals (crypto); otherwise whole units.
	Fractional bool `json:"fractional,omitempty"`
}

type SizeResult struct {
	Quantity    float64 `json:"quantity"`
	Stop        float64 `json:"stop"`
	RiskAmount  float64 `json:"risk_amount"`
	PositionPct float64 `json:"position_pct"`
	CappedBy    string  `json:"capped_by,omitempty"` // max_weight | buying_power
}

const (
	defaultRiskPct   = 1.0
	defaultATRMult   = 2.0
	defaultMaxWeight = 20.0
)

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// Size returns the quantity whose stop-out loss is RiskPct of equity, capped
// by the maximum position weight and by buying power. The quantity can be
// zero when a cap or the distance to the stop leaves less than one unit.
func Size(req SizeRequest, buyingPower float64) (SizeResult, error) {
	side := req.Side
	if side != "buy" && side != "sell" {
		return SizeResult{}, errors.New("side must be buy or sell")
	}
	if !finitePositive(req.Equity) || !finitePositive(req.Entry) {
		return SizeResult{}, errors.New("equity and entry must be above zero")
	}
	risk := req.RiskPct
	if risk == 0 {
		risk = defaultRiskPct
	}
	if math.IsNaN(risk) || risk < 0.1 || risk > 5 {
		return SizeResult{}, errors.New("risk_pct must be between 0.1 and 5")
	}
	maxWeight := req.MaxWeight
	if maxWeight == 0 {
		maxWeight = defaultMaxWeight
	}
	if math.IsNaN(maxWeight) || maxWeight <= 0 || maxWeight > 100 {
		return SizeResult{}, errors.New("max_weight_pct must be between 0 and 100")
	}

	stop := req.Stop
	if stop == 0 {
		mult := req.ATRMult
		if mult == 0 {
			mult = defaultATRMult
		}
		if !finitePositive(req.ATR) || !finitePositive(mult) {
			return SizeResult{}, errors.New("give a stop, or an ATR to derive one")
		}
		if side == "buy" {
			stop = req.Entry - mult*req.ATR
		} else {
			stop = req.Entry + mult*req.ATR
		}
	}
	if !finitePositive(stop) {
		return SizeResult{}, errors.New("the stop would be at or below zero; use a smaller ATR multiple or give a stop")
	}
	if (side == "buy" && stop >= req.Entry) || (side == "sell" && stop <= req.Entry) {
		return SizeResult{}, fmt.Errorf("a %s needs its stop on the losing side of the entry", side)
	}
	perUnit := math.Abs(req.Entry - stop)

	fit := func(q float64) float64 {
		if req.Fractional {
			return math.Floor(q*1e8) / 1e8
		}
		return math.Floor(q)
	}
	qty := fit(req.Equity * risk / 100 / perUnit)
	capped := ""
	if limit := fit(req.Equity * maxWeight / 100 / req.Entry); qty > limit {
		qty, capped = limit, "max_weight"
	}
	if side == "buy" {
		if limit := fit(buyingPower / req.Entry); qty > limit {
			qty, capped = math.Max(limit, 0), "buying_power"
		}
	}
	return SizeResult{
		Quantity: round8(qty), Stop: round8(stop), RiskAmount: round8(qty * perUnit),
		PositionPct: math.Round(qty*req.Entry/req.Equity*10000) / 100, CappedBy: capped,
	}, nil
}
