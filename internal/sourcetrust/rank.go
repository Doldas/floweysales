package sourcetrust

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Ranking profiles a user chooses. They change how much community discussion
// weighs in the order of results. They never change a source's type or
// evidence role: sentiment-first cannot turn an opinion into a fact.
const (
	ProfileFactsFirst     = "facts_first" // the default
	ProfileBalanced       = "balanced"
	ProfileSentimentFirst = "sentiment_first"
)

// ValidProfile reports whether p is a profile name or empty (the default).
func ValidProfile(p string) bool {
	return p == "" || p == ProfileFactsFirst || p == ProfileBalanced || p == ProfileSentimentFirst
}

// Intent is what a search is for. The same query is ranked differently for a
// financial figure, for public reaction and for a general lookup.
type Intent struct {
	Kind       string  `json:"kind"` // financial_fact, sentiment or general
	Latest     bool    `json:"latest,omitempty"`
	Historical bool    `json:"historical,omitempty"`
	Period     *Period `json:"-"`
	PeriodText string  `json:"period,omitempty"`
}

// Intent kinds.
const (
	IntentFinancial = "financial_fact"
	IntentSentiment = "sentiment"
	IntentGeneral   = "general"
)

var (
	sentimentWords = []string{"sentiment", "reddit", "what do people think", "what people think", "opinion", "reaction", "community", "discussion", "buzz", "wallstreetbets", "stocktwits", "forum", "retail investors say", "public perception", "thread"}
	financialWords = []string{"revenue", "eps", "earnings", "net income", "profit", "margin", "cash flow", "free cash", "10-k", "10-q", "20-f", "annual report", "interim report", "quarterly report", "balance sheet", "income statement", "dividend", "guidance", "ebitda", "operating income", "sales", "filing", "fiscal"}
	latestWords    = []string{"latest", "today", "current", "currently", "right now", "recent", "recently", "this week", "this month", "breaking", "newest", "most recent", "up to date", "just announced"}
	historyWords   = []string{"historical", "history of", "back in", "in the past", "years ago"}
)

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// DetectIntent reads a query with fixed rules.
func DetectIntent(query string) Intent {
	q := " " + strings.ToLower(query) + " "
	in := Intent{Kind: IntentGeneral}
	if p, ok := ExtractPeriod(query); ok {
		in.Period, in.PeriodText = p, p.Label()
	}
	switch {
	case containsAny(q, sentimentWords):
		in.Kind = IntentSentiment
	case containsAny(q, financialWords) || in.Period != nil:
		in.Kind = IntentFinancial
	}
	if in.Period == nil && containsAny(q, latestWords) {
		in.Latest = true
	}
	if in.Period != nil || containsAny(q, historyWords) {
		in.Historical = true
	}
	return in
}

// Describe is the policy in a sentence, shown with the results.
func (in Intent) Describe(profile string, windowDays int) string {
	var parts []string
	switch in.Kind {
	case IntentFinancial:
		parts = append(parts, "financial facts: official and primary sources first")
	case IntentSentiment:
		parts = append(parts, "public reaction: community discussion counts, labeled as sentiment")
	default:
		parts = append(parts, "general lookup")
	}
	switch {
	case in.Period != nil:
		parts = append(parts, "the requested period "+in.PeriodText+" outranks recency")
	case in.Latest:
		parts = append(parts, "latest requested, so newer sources rank higher")
	}
	if profile == "" {
		profile = ProfileFactsFirst
	}
	parts = append(parts, "profile "+strings.ReplaceAll(profile, "_", "-"))
	if windowDays > 0 && in.Latest {
		parts = append(parts, fmt.Sprintf("window %d days with older sources kept as fallback", windowDays))
	}
	return strings.Join(parts, "; ")
}

var factAuthority = map[string]float64{
	TypeOfficialFiling: 1, TypeExchange: .9, TypeGovernmentData: .95, TypePeerReviewed: .9, TypeFirstPartyDataset: .85,
	TypeCompanyIR: .75, TypeEstablishedNews: .55, TypeProfessionalAnalysis: .35, TypeUnknown: .2, TypeMarketing: .1, TypeCommunity: .1,
}

var sentimentAuthority = map[string]float64{
	TypeCommunity: 1, TypeEstablishedNews: .6, TypeProfessionalAnalysis: .6, TypeCompanyIR: .3, TypeUnknown: .3, TypeMarketing: .1,
	TypeOfficialFiling: .3, TypeExchange: .3, TypeGovernmentData: .3, TypePeerReviewed: .3, TypeFirstPartyDataset: .3,
}

// halfLifeDays is how fast each kind of source goes stale: after this many days
// a source of the type counts half as fresh. Category-specific by design: an
// annual report stays relevant for over a year, a forum thread for days.
var halfLifeDays = map[string]float64{
	TypeOfficialFiling: 400, TypeExchange: 90, TypeGovernmentData: 365, TypePeerReviewed: 1500, TypeFirstPartyDataset: 180,
	TypeCompanyIR: 120, TypeEstablishedNews: 21, TypeProfessionalAnalysis: 60, TypeCommunity: 7, TypeMarketing: 30, TypeUnknown: 60,
}

// Candidate is one result to rank.
type Candidate struct {
	Index   int // position the provider gave it
	URL     string
	Title   string
	Snippet string
	Meta    *Meta
}

// Options tunes a ranking.
type Options struct {
	Profile    string
	Intent     Intent
	WindowDays int // user-visible freshness window for latest questions; 0 means none
	Now        time.Time
	Limit      int // keep at most this many; 0 keeps all
}

// Filtered says what was left out of the visible results and why.
type Filtered struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

func normWords(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func titleKey(title string) string {
	t := title
	for _, sep := range []string{" | ", " - ", " – ", " — "} {
		if i := strings.LastIndex(t, sep); i > 0 {
			t = t[:i]
		}
	}
	k := normWords(t)
	if len(strings.Fields(k)) < 4 {
		return ""
	}
	return k
}

func snippetKey(s string) string {
	k := normWords(s)
	if len(k) < 80 {
		return ""
	}
	return k[:80]
}

// Dedupe merges syndicated and copied variants (same headline or same opening
// text on different addresses) into one candidate. The kept one has the higher
// authority, then the earlier publication date, then the earlier position; the
// others are listed under its AlsoSeenAt and in the filtered list. It does not
// hide the original provenance.
func Dedupe(cands []*Candidate) ([]*Candidate, []Filtered) {
	better := func(a, b *Candidate) bool {
		aa, ba := factAuthority[a.Meta.Type], factAuthority[b.Meta.Type]
		if aa != ba {
			return aa > ba
		}
		at, aok := ParseDate(a.Meta.PublishedAt)
		bt, bok := ParseDate(b.Meta.PublishedAt)
		if aok && bok && !at.Equal(bt) {
			return at.Before(bt)
		}
		if aok != bok {
			return aok
		}
		return a.Index < b.Index
	}
	var kept []*Candidate
	var filtered []Filtered
	byKey := map[string]*Candidate{}
	for _, c := range cands {
		matched := (*Candidate)(nil)
		for _, k := range []string{"t:" + titleKey(c.Title), "s:" + snippetKey(c.Snippet)} {
			if k == "t:" || k == "s:" {
				continue
			}
			if o, ok := byKey[k]; ok {
				matched = o
				break
			}
		}
		if matched == nil {
			kept = append(kept, c)
			for _, k := range []string{"t:" + titleKey(c.Title), "s:" + snippetKey(c.Snippet)} {
				if k != "t:" && k != "s:" {
					byKey[k] = c
				}
			}
			continue
		}
		keep, drop := matched, c
		if better(c, matched) {
			keep, drop = c, matched
			for i := range kept {
				if kept[i] == matched {
					kept[i] = c
				}
			}
			for k, v := range byKey {
				if v == matched {
					byKey[k] = c
				}
			}
			keep.Meta.AlsoSeenAt = append(keep.Meta.AlsoSeenAt, matched.Meta.AlsoSeenAt...)
		}
		keep.Meta.AlsoSeenAt = append(keep.Meta.AlsoSeenAt, drop.URL)
		filtered = append(filtered, Filtered{URL: drop.URL, Reason: "syndicated or copied variant of " + keep.URL + "; counted as one underlying source"})
	}
	return kept, filtered
}

func ageDays(raw string, now time.Time) (float64, bool) {
	t, ok := ParseDate(raw)
	if !ok {
		return 0, false
	}
	d := now.Sub(t).Hours() / 24
	if d < 0 {
		d = 0
	}
	return d, true
}

func typeLabel(t string) string { return strings.ReplaceAll(t, "_", " ") }

// Score computes the score and the named factors of one candidate. The factors
// are the explanation; the number only orders results of one search and means
// nothing outside it.
func Score(c *Candidate, n int, o Options) (float64, []string) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	in := o.Intent
	var why []string
	score := 0.0

	if n > 0 {
		score += 20 * (1 - float64(c.Index)/float64(n))
	}

	// Source authority for this kind of question. Profile decides how much
	// community discussion counts when the question is not about sentiment.
	var s float64
	switch o.Profile {
	case ProfileBalanced:
		s = .3
	case ProfileSentimentFirst:
		s = .6
	}
	if in.Kind == IntentSentiment {
		s = 1
		if o.Profile == "" || o.Profile == ProfileFactsFirst {
			s = .8
		}
	}
	auth := (1-s)*factAuthority[c.Meta.Type] + s*sentimentAuthority[c.Meta.Type]
	score += 40 * auth
	why = append(why, fmt.Sprintf("%s (%s)", typeLabel(c.Meta.Type), strings.ReplaceAll(c.Meta.Role, "_", " ")))

	// Reporting period: the right quarter matters more than a recent page.
	if in.Period != nil && c.Meta.Type != TypeCommunity {
		var got *Period
		if p, ok := periodFromLabel(c.Meta.Period); ok {
			got = p
		}
		switch Compare(in.Period, got) {
		case PeriodMatch:
			score += 30
			why = append(why, "covers the requested period "+in.PeriodText)
		case PeriodRelated:
			score += 8
			why = append(why, "covers "+c.Meta.Period+", related to the requested "+in.PeriodText+" but not the same period")
		case PeriodMismatch:
			score -= 25
			why = append(why, "covers "+c.Meta.Period+", not the requested "+in.PeriodText)
		default:
			why = append(why, "period not stated, so a match with "+in.PeriodText+" is unconfirmed")
		}
	}

	// Freshness comes from the publication or as-of date, never from a page's
	// "modified" stamp or a file date.
	basis, basisName := c.Meta.EffectiveAt, "as of"
	if basis == "" {
		basis, basisName = c.Meta.PublishedAt, "published"
	}
	weight := 12.0
	switch {
	case in.Latest:
		weight = 25
	case in.Historical:
		weight = 4
	case in.Kind == IntentSentiment:
		weight = 20
	}
	if age, ok := ageDays(basis, now); ok {
		hl := halfLifeDays[c.Meta.Type]
		if hl == 0 {
			hl = 60
		}
		fresh := math.Pow(.5, age/hl)
		score += weight * fresh
		why = append(why, fmt.Sprintf("%s %s (%d days ago; %s sources lose half their freshness weight in %d days)", basisName, dateOnly(basis), int(age), typeLabel(c.Meta.Type), int(hl)))
		if o.WindowDays > 0 && in.Latest && age > float64(o.WindowDays) {
			score -= 20
			why = append(why, fmt.Sprintf("outside the %d-day window; kept as a fallback", o.WindowDays))
		}
	} else {
		switch {
		case c.Meta.ModifiedAt != "":
			why = append(why, "page updated "+dateOnly(c.Meta.ModifiedAt)+"; the update date is not used as freshness because the original publication date is unknown")
		default:
			why = append(why, "publication date unknown; no freshness credit")
		}
		if in.Latest {
			score -= 5
			if o.WindowDays > 0 {
				score -= 3
			}
		}
	}

	switch c.Meta.EvidenceState {
	case StatePageRead, StatePDFText:
		score += 6
		why = append(why, "page content read")
	case StateVisionDerived:
		score += 3
		why = append(why, "includes vision-derived content")
	case StateAccessDenied:
		score -= 15
		why = append(why, "access denied; nothing was read")
	case StateReadFailed:
		score -= 8
		why = append(why, "could not be read")
	}
	return score, why
}

func dateOnly(raw string) string {
	if t, ok := ParseDate(raw); ok {
		return t.UTC().Format("2006-01-02")
	}
	return raw
}

func periodFromLabel(label string) (*Period, bool) {
	if label == "" {
		return nil, false
	}
	return ExtractPeriod(label)
}

// Rank orders candidates for the task, merges syndicated copies, and records
// rank and reasons on each Meta. What it leaves out of the first Limit results
// is returned with the reason, so nothing disappears silently.
func Rank(cands []*Candidate, o Options) ([]*Candidate, []Filtered) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	kept, filtered := Dedupe(cands)
	type scored struct {
		c *Candidate
		s float64
	}
	list := make([]scored, len(kept))
	for i, c := range kept {
		s, why := Score(c, len(cands), o)
		c.Meta.WhyRanked = why
		list[i] = scored{c, s}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].s != list[j].s {
			return list[i].s > list[j].s
		}
		return list[i].c.Index < list[j].c.Index
	})
	out := make([]*Candidate, 0, len(list))
	for i, e := range list {
		if o.Limit > 0 && i >= o.Limit {
			filtered = append(filtered, Filtered{URL: e.c.URL, Reason: fmt.Sprintf("below the top %d results of this search", o.Limit)})
			continue
		}
		e.c.Meta.Rank = len(out) + 1
		out = append(out, e.c)
	}
	return out, filtered
}
