package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"floweysales/internal/market"
	"floweysales/internal/portfolio"
	"floweysales/internal/trading"
)

// atrPeriod is the lookback of the ATR stop, on daily bars.
const atrPeriod = 14

type sizeRequest struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	RiskPct   float64 `json:"risk_pct"`
	Entry     float64 `json:"entry"` // default: the last close
	Stop      float64 `json:"stop"`
	ATRMult   float64 `json:"atr_mult"`
	MaxWeight float64 `json:"max_weight_pct"`
}

// registerPositionSizingRoutes serves POST /api/trading/size: how many units
// to trade so that being stopped out loses a fixed share of equity. It sizes
// a ticket; it places nothing.
func (e *Engine) registerPositionSizingRoutes() {
	e.mux.HandleFunc("POST /api/trading/size", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		defer r.Body.Close()
		var req sizeRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid sizing request"})
			return
		}
		symbol, err := market.CanonicalSymbol(req.Symbol)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid symbol"})
			return
		}
		side := strings.ToLower(req.Side)

		_, summary := e.paperAccount()
		in := trading.SizeRequest{
			Equity: summary.Equity, RiskPct: req.RiskPct, Entry: req.Entry, Stop: req.Stop, ATRMult: req.ATRMult,
			MaxWeight: req.MaxWeight, Side: side, Fractional: portfolio.ClassOf(symbol) == portfolio.Satellite,
		}
		// Daily bars give the default entry and, without a stop, the ATR.
		if in.Entry == 0 || in.Stop == 0 {
			series, err := e.flowHistory(r.Context(), symbol, e.parseProvider(""), market.Range3M)
			if err != nil {
				writeJSON(w, providerErrorStatus(err), map[string]any{"error": err.Error()})
				return
			}
			bars := dailyBars(series.Points)
			if in.Entry == 0 && len(bars) > 0 {
				in.Entry = bars[len(bars)-1].Close
			}
			if in.Stop == 0 {
				atr, ok := market.ATR(bars, atrPeriod)
				if !ok {
					writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Not enough price history for an ATR stop; give a stop price"})
					return
				}
				in.ATR = atr
			}
		}
		res, err := trading.Size(in, summary.BuyingPower)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		mult := in.ATRMult
		if mult == 0 && in.Stop == 0 {
			mult = 2
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"symbol": symbol, "side": side, "entry": in.Entry, "equity": summary.Equity, "risk_pct": riskOrDefault(req.RiskPct),
			"atr_mult": mult, "atr": in.ATR, "stop_source": stopSource(in.Stop), "result": res,
		})
	})
}

func riskOrDefault(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}

func stopSource(stop float64) string {
	if stop > 0 {
		return "given"
	}
	return "atr"
}
