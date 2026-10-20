package trading

import (
	"strings"
	"testing"
)

func TestPaperTradingKillSwitchRejectsNewOrders(t *testing.T) {
	req := SubmitRequest{
		Symbol: "TEST", Side: SideBuy, Type: TypeMarket,
		Quantity: 1, ReferencePrice: 100, BuyingPower: 1000,
		Cfg: Config{BaseCash: 1000, HaltNewOrders: true},
	}
	if _, err := NewOrder(req); err == nil || !strings.Contains(err.Error(), "halted") {
		t.Fatalf("halt must reject market buy: %v", err)
	}
	req.Side = SideSell
	req.HeldQuantity = 5
	if _, err := NewOrder(req); err == nil {
		t.Fatal("halt must reject sells as well as buys")
	}
	req.Cfg.HaltNewOrders = false
	if _, err := NewOrder(req); err != nil {
		t.Fatalf("simulation should resume when unhalted: %v", err)
	}
}
