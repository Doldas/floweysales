// Package strategy holds rule-based trading strategies for one instrument
// and the engine that replays them over daily bars. A strategy is data (a
// Strategy value, stored as JSON), so the same engine runs the built-in
// Fibonacci strategies and the ones users build in the interface.
//
// Rules only see bars up to the bar being decided. Swing points are
// confirmed after SwingBars bars on each side, so a Fibonacci level never
// moves backwards in time. A signal on a bar's close is filled at the next
// bar's open.
package strategy

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Operand types.
const (
	OpPrice     = "price"      // Field: open, high, low, close, volume
	OpConst     = "const"      // Value
	OpEMA       = "ema"        // Period, Field (default close)
	OpSMA       = "sma"        // Period, Field (default close)
	OpRSI       = "rsi"        // Period (Wilder)
	OpATR       = "atr"        // Period (Wilder)
	OpHighest   = "highest"    // highest high of the Period bars before this one
	OpLowest    = "lowest"     // lowest low of the Period bars before this one
	OpFibRetr   = "fib_retr"   // retracement level of the latest swing leg
	OpFibExt    = "fib_ext"    // extension level of the latest swing leg
	OpSwingHigh = "swing_high" // high of the latest swing leg
	OpSwingLow  = "swing_low"  // low of the latest swing leg
)

// Comparison operators.
const (
	CmpGT           = ">"
	CmpGTE          = ">="
	CmpLT           = "<"
	CmpLTE          = "<="
	CmpCrossesAbove = "crosses_above"
	CmpCrossesBelow = "crosses_below"
)

// Directions and sizing modes.
const (
	Long        = "long"
	Short       = "short"
	SizeFixed   = "fixed_pct" // Value percent of equity per trade
	SizeRiskPct = "risk_pct"  // Value percent of equity lost if the stop is hit
)

// Limits that keep a stored strategy cheap to evaluate.
const (
	maxConditions = 8
	maxPeriod     = 400
	maxRatio      = 5.0
	maxNameLen    = 60
	maxDescLen    = 400
)

// Operand is one side of a condition: a price, an indicator, or a level
// derived from the latest swing leg. Ratio is the Fibonacci ratio.
type Operand struct {
	Type   string  `json:"type"`
	Field  string  `json:"field,omitempty"`
	Period int     `json:"period,omitempty"`
	Ratio  float64 `json:"ratio,omitempty"`
	Value  float64 `json:"value,omitempty"`
}

// Condition compares two operands.
type Condition struct {
	Left  Operand `json:"left"`
	Op    string  `json:"op"`
	Right Operand `json:"right"`
}

// Level is a price fixed when a trade opens: an operand plus an offset in
// ATRs. A positive ATR offset moves the level further from the entry on a
// stop and further into profit on a target.
type Level struct {
	Of  Operand `json:"of"`
	ATR float64 `json:"atr,omitempty"`
}

// Sizing decides how much of the equity a trade uses.
type Sizing struct {
	Mode  string  `json:"mode"`
	Value float64 `json:"value"`
}

// Strategy is a complete rule set. Entry conditions must all hold; any one
// exit condition closes the trade.
type Strategy struct {
	ID          string      `json:"id,omitempty"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Family      string      `json:"family,omitempty"`  // "fibonacci" or "persona" for the built-in ones
	Persona     string      `json:"persona,omitempty"` // the character whose strategy this is (built-in only)
	Direction   string      `json:"direction"`
	SwingBars   int         `json:"swing_bars,omitempty"`  // bars each side that confirm a swing, default 5
	MinLegATR   float64     `json:"min_leg_atr,omitempty"` // smallest swing leg, in ATRs, default 2
	Entry       []Condition `json:"entry"`
	Exit        []Condition `json:"exit,omitempty"`
	Stop        *Level      `json:"stop,omitempty"`
	Target      *Level      `json:"target,omitempty"`
	MaxBars     int         `json:"max_bars,omitempty"` // close after this many bars, 0 = no limit
	Sizing      Sizing      `json:"sizing"`
}

var priceFields = map[string]bool{"open": true, "high": true, "low": true, "close": true, "volume": true}

// Normalize fills defaults and rejects a strategy the engine cannot run.
func (s *Strategy) Normalize() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	if s.Name == "" || len(s.Name) > maxNameLen {
		return fmt.Errorf("name must be 1 to %d characters", maxNameLen)
	}
	if len(s.Description) > maxDescLen {
		return fmt.Errorf("description is longer than %d characters", maxDescLen)
	}
	switch s.Direction {
	case "":
		s.Direction = Long
	case Long, Short:
	default:
		return errors.New("direction must be long or short")
	}
	if s.SwingBars == 0 {
		s.SwingBars = 5
	}
	if s.SwingBars < 2 || s.SwingBars > 20 {
		return errors.New("swing_bars must be from 2 to 20")
	}
	if s.MinLegATR == 0 {
		s.MinLegATR = 2
	}
	if s.MinLegATR < 0 || s.MinLegATR > 20 {
		return errors.New("min_leg_atr must be from 0 to 20")
	}
	if len(s.Entry) == 0 {
		return errors.New("add at least one entry condition")
	}
	if len(s.Entry) > maxConditions || len(s.Exit) > maxConditions {
		return fmt.Errorf("at most %d conditions per list", maxConditions)
	}
	for i := range s.Entry {
		if err := s.Entry[i].normalize(); err != nil {
			return fmt.Errorf("entry %d: %w", i+1, err)
		}
	}
	for i := range s.Exit {
		if err := s.Exit[i].normalize(); err != nil {
			return fmt.Errorf("exit %d: %w", i+1, err)
		}
	}
	for name, l := range map[string]*Level{"stop": s.Stop, "target": s.Target} {
		if l == nil {
			continue
		}
		if err := l.Of.normalize(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if math.IsNaN(l.ATR) || math.Abs(l.ATR) > 20 {
			return fmt.Errorf("%s: atr offset must be between -20 and 20", name)
		}
	}
	if s.MaxBars < 0 || s.MaxBars > 2000 {
		return errors.New("max_bars must be from 0 to 2000")
	}
	if s.Sizing.Mode == "" {
		s.Sizing = Sizing{Mode: SizeFixed, Value: 100}
	}
	switch s.Sizing.Mode {
	case SizeFixed:
		if s.Sizing.Value <= 0 || s.Sizing.Value > 100 {
			return errors.New("sizing: percent of equity must be above 0 and at most 100")
		}
	case SizeRiskPct:
		if s.Sizing.Value <= 0 || s.Sizing.Value > 10 {
			return errors.New("sizing: risk per trade must be above 0 and at most 10 percent")
		}
		if s.Stop == nil {
			return errors.New("sizing by risk needs a stop")
		}
	default:
		return errors.New("sizing mode must be fixed_pct or risk_pct")
	}
	if s.Stop == nil && s.Target == nil && len(s.Exit) == 0 && s.MaxBars == 0 {
		return errors.New("add a way out: a stop, a target, an exit condition or a bar limit")
	}
	return nil
}

func (c *Condition) normalize() error {
	switch c.Op {
	case CmpGT, CmpGTE, CmpLT, CmpLTE, CmpCrossesAbove, CmpCrossesBelow:
	default:
		return fmt.Errorf("unknown comparison %q", c.Op)
	}
	if err := c.Left.normalize(); err != nil {
		return err
	}
	return c.Right.normalize()
}

func (o *Operand) normalize() error {
	needPeriod := func() error {
		if o.Period < 1 || o.Period > maxPeriod {
			return fmt.Errorf("%s needs a period from 1 to %d", o.Type, maxPeriod)
		}
		return nil
	}
	switch o.Type {
	case OpPrice:
		if !priceFields[o.Field] {
			return errors.New("price needs a field: open, high, low, close or volume")
		}
	case OpConst:
		if math.IsNaN(o.Value) || math.IsInf(o.Value, 0) || math.Abs(o.Value) > 1e12 {
			return errors.New("constant is out of range")
		}
	case OpEMA, OpSMA:
		if o.Field == "" {
			o.Field = "close"
		}
		if !priceFields[o.Field] {
			return errors.New("average needs a field: open, high, low, close or volume")
		}
		return needPeriod()
	case OpRSI, OpATR, OpHighest, OpLowest:
		return needPeriod()
	case OpFibRetr, OpFibExt:
		if math.IsNaN(o.Ratio) || o.Ratio < 0 || o.Ratio > maxRatio {
			return fmt.Errorf("fibonacci ratio must be from 0 to %g", maxRatio)
		}
	case OpSwingHigh, OpSwingLow:
	default:
		return fmt.Errorf("unknown operand type %q", o.Type)
	}
	return nil
}
