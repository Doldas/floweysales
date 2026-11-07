package sources

import (
	"math"
	"regexp"
	"strings"
)

// Finding kinds the team summary check adds.
const (
	SummaryFigureUnsupported = "summary_figure_not_in_members"
	SummaryMisattributed     = "summary_misattributed"
)

// SummaryMember is one member's final answer, as the chair received it.
type SummaryMember struct {
	Name string
	Text string
}

// summaryFigure is a figure in a chair sentence that is worth tracing: a
// percentage, a currency amount, a scaled amount or a number with at least
// three digits. Years, small counts ("two of three members") and labels are
// skipped, because the chair legitimately counts members itself.
func summaryFigures(sentence string) []numToken {
	var out []numToken
	for _, t := range numberTokens(sentence) {
		plain := t.currency == "" && !t.percent && t.scale == 1
		if plain {
			if t.value >= 1900 && t.value <= 2100 && t.value == math.Trunc(t.value) {
				continue
			}
			if math.Abs(t.value) < 100 {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func tokenMatches(a, b numToken) bool {
	if a.percent != b.percent {
		return false
	}
	av, bv := a.amount(1), b.amount(1)
	if a.percent {
		av, bv = a.value, b.value
	}
	if av == bv {
		return true
	}
	// Tolerate rounding the chair applied: equal at the coarser precision.
	tol := math.Max(math.Abs(av), math.Abs(bv)) * 0.005
	return math.Abs(av-bv) <= tol && roundedForm(a, b)
}

// roundedForm reports whether one token is written with a scale word, so
// 1.2 billion matches 1,203,400,000.
func roundedForm(a, b numToken) bool { return a.scale != 1 || b.scale != 1 }

func namesMember(sentence, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	return err == nil && re.MatchString(sentence)
}

// CheckSummary traces every notable figure of a chair summary to the member
// answers. The chair has no sources of its own: it may restate what members
// wrote, and nothing else. A figure that no member wrote is flagged, and so is
// a figure the sentence ties to one member when only other members wrote it.
// It is deterministic and calls no model; it checks that a figure was said,
// not that it is true. A summary with no flagged figure is reported as clean
// by an empty list.
func CheckSummary(summary string, members []SummaryMember) []Finding {
	toks := make([][]numToken, len(members))
	for i, m := range members {
		toks[i] = numberTokens(m.Text)
	}
	var out []Finding
	for _, sentence := range splitSentences(summary) {
		figs := summaryFigures(sentence)
		if len(figs) == 0 {
			continue
		}
		named := -1
		for i, m := range members {
			if namesMember(sentence, m.Name) {
				if named >= 0 {
					named = -2 // two members named: the sentence compares them
					break
				}
				named = i
			}
		}
		for _, f := range figs {
			var by []int
			for i := range members {
				for _, t := range toks[i] {
					if tokenMatches(f, t) {
						by = append(by, i)
						break
					}
				}
			}
			switch {
			case len(by) == 0:
				out = append(out, Finding{Kind: SummaryFigureUnsupported, Sentence: sentence,
					Detail: "The figure " + f.text + " does not appear in any member answer. The chair may only restate what the members wrote."})
			case named >= 0 && !containsInt(by, named):
				out = append(out, Finding{Kind: SummaryMisattributed, Sentence: sentence,
					Detail: "The figure " + f.text + " is attributed to " + members[named].Name + ", whose answer does not contain it; it appears in " + memberNames(members, by) + "."})
			}
		}
	}
	return out
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func memberNames(members []SummaryMember, idx []int) string {
	names := make([]string, len(idx))
	for i, j := range idx {
		names[i] = members[j].Name
	}
	return strings.Join(names, ", ")
}
