package trading

import (
	"math"
	"strings"
	"testing"
)

func TestPaperOrderNotionalLimit(t *testing.T) {
	base := SubmitRequest{
		Symbol: "TEST", Side: SideBuy, Type: TypeMarket, TIF: TIFGTC,
		Quantity: 10, ReferencePrice: 100, BuyingPower: 100000,
		Cfg: Config{BaseCash: 100000, MaxOrderNotional: 1000},
	}
	if _, err := NewOrder(base); err != nil {
		t.Fatalf("exactly at cap should pass: %v", err)
	}
	base.Quantity = 10.01
	if _, err := NewOrder(base); err == nil || !strings.Contains(err.Error(), "paper-trading limit") {
		t.Fatalf("expected over-limit order to fail, got %v", err)
	}
	base.Type = TypeLimit
	base.Quantity = 2
	base.ReferencePrice = 50
	base.LimitPrice = 600
	if _, err := NewOrder(base); err == nil {
		t.Fatal("resting limit must respect worst-case notional")
	}
	base.Cfg.MaxOrderNotional = 0
	if _, err := NewOrder(base); err != nil {
		t.Fatalf("zero means disabled: %v", err)
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1)} {
		base.Cfg.MaxOrderNotional = bad
		if _, err := NewOrder(base); err == nil {
			t.Fatalf("invalid risk limit %v accepted", bad)
		}
	}
}

func TestPaperSymbolAllocationLimit(t *testing.T) {
	req := SubmitRequest{Symbol: "TEST", Side: SideBuy, Type: TypeMarket, Quantity: 10, ReferencePrice: 100, BuyingPower: 100000, PortfolioEquity: 10000, SymbolExposure: 1000, Cfg: Config{MaxSymbolAllocationPct: 20}}
	if _, err := NewOrder(req); err != nil {
		t.Fatalf("exactly at cap must pass: %v", err)
	}
	req.Quantity = 10.01
	if _, err := NewOrder(req); err == nil {
		t.Fatal("above 20 percent must be rejected")
	}
	req.Cfg.MaxSymbolAllocationPct = 0
	if _, err := NewOrder(req); err != nil {
		t.Fatalf("disabled cap must pass: %v", err)
	}
	for _, bad := range []float64{-1, 101, math.NaN()} {
		req.Cfg.MaxSymbolAllocationPct = bad
		if _, err := NewOrder(req); err == nil {
			t.Fatalf("invalid allocation cap %v accepted", bad)
		}
	}
}
