package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"

	"floweysales/internal/trading"
)

// tradingSettings are the paper-trading limits and costs an administrator can
// change in Settings. Saved values replace the PAPER_* and TRADE_COMMISSION_*
// ones from the environment, take effect at once and survive a restart. The
// starting cash and the per-symbol sector and volume maps stay in the
// environment: changing them would rewrite the history of the account.
type tradingSettings struct {
	AllowShort             bool    `json:"allow_short"`
	HaltNewOrders          bool    `json:"halt_new_orders"`
	RequirePaperApproval   bool    `json:"require_paper_approval"`
	ApprovalTTLMinutes     int     `json:"approval_ttl_minutes"`
	MaxOrderNotional       float64 `json:"max_order_notional"`
	MaxSymbolAllocationPct float64 `json:"max_symbol_allocation_pct"`
	MaxSectorAllocationPct float64 `json:"max_sector_allocation_pct"`
	MaxDailyVolumePct      float64 `json:"max_daily_volume_pct"`
	MaxDrawdownPct         float64 `json:"max_drawdown_pct"`
	CommissionPct          float64 `json:"commission_pct"`
	CommissionFixed        float64 `json:"commission_fixed"`
}

func tradingSettingsOf(c trading.Config) tradingSettings {
	return tradingSettings{AllowShort: c.AllowShort, HaltNewOrders: c.HaltNewOrders, RequirePaperApproval: c.RequirePaperApproval,
		ApprovalTTLMinutes: int(c.ApprovalTTLSeconds() / 60), MaxOrderNotional: c.MaxOrderNotional,
		MaxSymbolAllocationPct: c.MaxSymbolAllocationPct, MaxSectorAllocationPct: c.MaxSectorAllocationPct,
		MaxDailyVolumePct: c.MaxDailyVolumePct, MaxDrawdownPct: c.MaxDrawdownPct,
		CommissionPct: c.Commission.Pct, CommissionFixed: c.Commission.Fixed}
}

func (s tradingSettings) applyTo(c trading.Config) trading.Config {
	c.AllowShort, c.HaltNewOrders, c.RequirePaperApproval = s.AllowShort, s.HaltNewOrders, s.RequirePaperApproval
	c.ApprovalTTLMinutes, c.MaxOrderNotional = s.ApprovalTTLMinutes, s.MaxOrderNotional
	c.MaxSymbolAllocationPct, c.MaxSectorAllocationPct = s.MaxSymbolAllocationPct, s.MaxSectorAllocationPct
	c.MaxDailyVolumePct, c.MaxDrawdownPct = s.MaxDailyVolumePct, s.MaxDrawdownPct
	c.Commission = trading.Commission{Pct: s.CommissionPct, Fixed: s.CommissionFixed}
	return c
}

func finiteBetween(v, lo, hi float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= lo && v <= hi
}

func (s tradingSettings) validate() string {
	switch {
	case s.ApprovalTTLMinutes < 1 || s.ApprovalTTLMinutes > trading.MaxApprovalTTLMinutes:
		return "Approval lifetime must be from 1 minute to 7 days"
	case !finiteBetween(s.MaxOrderNotional, 0, 1e12):
		return "Largest order must be 0 (no limit) or a positive amount"
	case !finiteBetween(s.MaxSymbolAllocationPct, 0, 100), !finiteBetween(s.MaxSectorAllocationPct, 0, 100),
		!finiteBetween(s.MaxDailyVolumePct, 0, 100), !finiteBetween(s.MaxDrawdownPct, 0, 100):
		return "Percentage limits must be from 0 (off) to 100"
	case !finiteBetween(s.CommissionPct, 0, 100), !finiteBetween(s.CommissionFixed, 0, 1e6):
		return "Commission must not be negative"
	}
	return ""
}

// restoreTradingSettings applies the settings saved in Settings over the
// environment ones. It is called at boot, with the environment config.
func (e *Engine) restoreTradingSettings(ctx context.Context, cfg trading.Config) (trading.Config, bool) {
	if e.db == nil {
		return cfg, false
	}
	raw, err := e.db.GetTradingConfiguration(ctx)
	if err != nil {
		log.Printf("[Trading] cannot read saved settings: %v", err)
		return cfg, false
	}
	if raw == "" {
		return cfg, false
	}
	var s tradingSettings
	if err := json.Unmarshal([]byte(raw), &s); err != nil || s.validate() != "" {
		log.Printf("[Trading] ignoring invalid saved settings")
		return cfg, false
	}
	return s.applyTo(cfg), true
}

func (e *Engine) tradingSettingsView() map[string]any {
	// Environment values are what "Reset" goes back to.
	return map[string]any{"settings": tradingSettingsOf(e.tradingConfig()), "defaults": tradingSettingsOf(e.envTradingCfg), "saved": e.tradingSaved}
}

func (e *Engine) registerTradingSettingsRoutes() {
	e.mux.HandleFunc("GET /api/admin/trading-settings", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		e.tradingMu.Lock()
		view := e.tradingSettingsView()
		e.tradingMu.Unlock()
		writeJSON(w, http.StatusOK, view)
	})
	e.mux.HandleFunc("PUT /api/admin/trading-settings", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		if e.db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "No database"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var s tradingSettings
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil || dec.Decode(new(any)) != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid trading settings"})
			return
		}
		if msg := s.validate(); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		raw, _ := json.Marshal(s)
		if err := e.db.SaveTradingConfiguration(r.Context(), string(raw)); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Unable to save the trading settings"})
			return
		}
		e.tradingMu.Lock()
		e.tradingCfg = s.applyTo(e.tradingCfg)
		e.tradingSaved = true
		view := e.tradingSettingsView()
		e.tradingMu.Unlock()
		if user, ok := e.currentUser(r); ok {
			log.Printf("[Trading] %s saved paper-trading settings: %s", user.Email, raw)
		}
		writeJSON(w, http.StatusOK, view)
	})
	e.mux.HandleFunc("DELETE /api/admin/trading-settings", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		if e.db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "No database"})
			return
		}
		if err := e.db.DeleteTradingConfiguration(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Unable to reset the trading settings"})
			return
		}
		e.tradingMu.Lock()
		e.tradingCfg = e.envTradingCfg
		e.tradingSaved = false
		view := e.tradingSettingsView()
		e.tradingMu.Unlock()
		if user, ok := e.currentUser(r); ok {
			log.Printf("[Trading] %s reset paper-trading settings to the environment values", user.Email)
		}
		writeJSON(w, http.StatusOK, view)
	})
}
