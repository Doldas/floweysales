// Package grounding checks that the figures an agent states appear in its
// evidence. It is a mechanical check against invented numbers, not a
// semantic fact check.
package grounding

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	// MaxEvidence bounds the tool-result text callers keep per attempt.
	MaxEvidence          = 512 << 10
	maxUnsupportedListed = 5
)

var (
	// A number, optionally signed, with thousands separators and decimals,
	// plus what directly follows it (%, a scale word or a letter).
	figurePattern         = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}._])(-?\d{1,3}(?:,\d{3})+(?:\.\d+)?|-?\d+(?:\.\d+)?)(\s?%|\s?(?:thousand|million|billion|trillion|bn|mn|[kmbt])\b)?`)
	evidenceNumberPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?`)
)

// Figure is a number stated in an answer.
type Figure struct {
	Text     string
	Value    float64
	decimals int
	percent  bool
	scale    float64
}

var scaleWords = map[string]float64{"thousand": 1e3, "k": 1e3, "million": 1e6, "mn": 1e6, "m": 1e6, "billion": 1e9, "bn": 1e9, "b": 1e9, "trillion": 1e12, "t": 1e12}

// Figures returns the figures in an answer worth checking: decimals,
// percentages, scaled amounts and numbers of at least 100. Years, small
// counts and numbers inside labels such as FY2025 or Q3 are skipped.
func Figures(text string) []Figure {
	var out []Figure
	for _, m := range figurePattern.FindAllStringSubmatchIndex(text, -1) {
		raw := text[m[4]:m[5]]
		suffix := ""
		if m[6] >= 0 {
			suffix = strings.ToLower(strings.TrimSpace(text[m[6]:m[7]]))
		}
		// A letter right after the number (other than a scale word) makes it a label.
		if suffix == "" && m[5] < len(text) && isLetter(text[m[5]]) {
			continue
		}
		clean := strings.ReplaceAll(raw, ",", "")
		value, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			continue
		}
		f := Figure{Text: strings.TrimSpace(text[m[4]:m[1]]), Value: value, scale: 1}
		if i := strings.IndexByte(clean, '.'); i >= 0 {
			f.decimals = len(clean) - i - 1
		}
		switch {
		case suffix == "%":
			f.percent = true
		case suffix != "":
			f.scale = scaleWords[suffix]
		}
		abs := math.Abs(value)
		isYear := f.decimals == 0 && !f.percent && f.scale == 1 && abs >= 1900 && abs <= 2100 && !strings.Contains(raw, ",")
		if isYear || (f.decimals == 0 && !f.percent && f.scale == 1 && abs < 100) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Amount is the figure with its scale word applied: 12.5B is 12500000000.
func (f Figure) Amount() float64 { return f.Value * f.scale }

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// EvidenceNumbers extracts every number in the evidence text.
func EvidenceNumbers(evidence string) []float64 {
	var out []float64
	for _, raw := range evidenceNumberPattern.FindAllString(strings.ReplaceAll(evidence, ",", ""), -1) {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// Supported reports whether a figure matches an evidence number at the
// figure's own precision, as a fraction behind a percentage, or in another
// scale (12.5 billion for 12500000000, or 12,500 million).
func (f Figure) Supported(evidence []float64) bool {
	target := f.Value * f.scale
	unit := math.Pow(10, -float64(f.decimals))
	near := func(stated, actual float64) bool {
		// Rounding to the stated precision, with a little room for float noise.
		return math.Abs(stated-actual) <= unit/2+1e-9*math.Max(1, math.Abs(actual))
	}
	for _, e := range evidence {
		if near(f.Value, e) {
			return true
		}
		if f.scale != 1 {
			// Evidence may be in units, thousands or millions.
			for _, s := range []float64{1, 1e3, 1e6} {
				if math.Abs(target-e*s) <= unit/2*f.scale {
					return true
				}
			}
		}
		if f.percent && near(f.Value, e*100) {
			return true
		}
		if f.scale == 1 && !f.percent {
			for _, s := range []float64{1e3, 1e6, 1e9} {
				if near(f.Value, e/s) {
					return true
				}
			}
		}
	}
	return false
}

// Check returns an error naming the answer's unsupported figures.
func Check(answer string, evidence ...string) error {
	numbers := EvidenceNumbers(strings.Join(evidence, "\n"))
	var missing []string
	seen := map[string]bool{}
	for _, f := range Figures(answer) {
		if f.Supported(numbers) || seen[f.Text] {
			continue
		}
		seen[f.Text] = true
		missing = append(missing, f.Text)
	}
	if len(missing) == 0 {
		return nil
	}
	listed := missing
	if len(listed) > maxUnsupportedListed {
		listed = listed[:maxUnsupportedListed]
	}
	more := ""
	if extra := len(missing) - len(listed); extra > 0 {
		more = fmt.Sprintf(" and %d more", extra)
	}
	return fmt.Errorf("%d figure(s) not found in tool results or input: %s%s", len(missing), strings.Join(listed, ", "), more)
}
