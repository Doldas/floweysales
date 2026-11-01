package trading

import (
	"math"
	"testing"
)

func TestSize(t *testing.T) {
	base := SizeRequest{Equity: 100_000, Entry: 100, Stop: 95, Side: "buy"}
	with := func(f func(*SizeRequest)) SizeRequest { r := base; f(&r); return r }
	cases := []struct {
		name         string
		req          SizeRequest
		power        float64
		qty, stop    float64
		capped       string
		risk, weight float64
	}{
		{"long with stop, uncapped by choosing a wide cap", with(func(r *SizeRequest) { r.MaxWeight = 100 }), 1e9, 200, 95, "", 1000, 20},
		{"max weight caps a tight stop", with(func(r *SizeRequest) { r.Stop = 99 }), 1e9, 200, 99, "max_weight", 200, 20},
		{"buying power caps", with(func(r *SizeRequest) { r.MaxWeight = 100 }), 10_000, 100, 95, "buying_power", 500, 10},
		{"short ignores buying power", with(func(r *SizeRequest) { r.Side = "sell"; r.Stop = 105; r.MaxWeight = 100 }), 0, 200, 105, "", 1000, 20},
		{"ATR stop long", with(func(r *SizeRequest) { r.Stop = 0; r.ATR = 2.5; r.MaxWeight = 100 }), 1e9, 200, 95, "", 1000, 20},
		{"ATR multiple 3 short", with(func(r *SizeRequest) { r.Side = "sell"; r.Stop = 0; r.ATR = 2; r.ATRMult = 3; r.MaxWeight = 100 }), 0, 166, 106, "", 996, 16.6},
		{"half a percent risk", with(func(r *SizeRequest) { r.RiskPct = 0.5; r.MaxWeight = 100 }), 1e9, 100, 95, "", 500, 10},
	}
	for _, c := range cases {
		got, err := Size(c.req, c.power)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Quantity != c.qty || got.Stop != c.stop || got.CappedBy != c.capped || got.RiskAmount != c.risk || math.Abs(got.PositionPct-c.weight) > 0.01 {
			t.Errorf("%s: got %+v, want qty %v stop %v capped %q risk %v weight %v", c.name, got, c.qty, c.stop, c.capped, c.risk, c.weight)
		}
	}
}

func TestSizeFractionalRoundsToEightDecimals(t *testing.T) {
	got, err := Size(SizeRequest{Equity: 10_000, Entry: 65_000, Stop: 63_000, Side: "buy", Fractional: true, MaxWeight: 100}, 1e9)
	if err != nil {
		t.Fatal(err)
	}
	if got.Quantity != 0.05 || got.RiskAmount != 100 {
		t.Fatalf("%+v", got)
	}
	whole, _ := Size(SizeRequest{Equity: 10_000, Entry: 65_000, Stop: 63_000, Side: "buy", MaxWeight: 100}, 1e9)
	if whole.Quantity != 0 {
		t.Fatalf("whole units cannot hold a fraction: %+v", whole)
	}
}

func TestSizeRejects(t *testing.T) {
	good := SizeRequest{Equity: 1000, Entry: 10, Stop: 9, Side: "buy"}
	bad := map[string]func(*SizeRequest){
		"zero distance":      func(r *SizeRequest) { r.Stop = 10 },
		"stop above a long":  func(r *SizeRequest) { r.Stop = 11 },
		"stop below a short": func(r *SizeRequest) { r.Side = "sell"; r.Stop = 9 },
		"no stop or atr":     func(r *SizeRequest) { r.Stop = 0 },
		"atr below zero":     func(r *SizeRequest) { r.Stop = 0; r.ATR = 6 },
		"risk too high":      func(r *SizeRequest) { r.RiskPct = 6 },
		"risk too low":       func(r *SizeRequest) { r.RiskPct = 0.05 },
		"no equity":          func(r *SizeRequest) { r.Equity = 0 },
		"nan entry":          func(r *SizeRequest) { r.Entry = math.NaN() },
		"bad side":           func(r *SizeRequest) { r.Side = "hold" },
		"weight over 100":    func(r *SizeRequest) { r.MaxWeight = 101 },
	}
	for name, mutate := range bad {
		r := good
		mutate(&r)
		if _, err := Size(r, 1e9); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
