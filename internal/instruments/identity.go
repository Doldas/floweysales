package instruments

import (
	"sort"
	"strings"

	"floweysales/internal/collections"
)

// How members relate across providers. A ticker alone never links two
// members: the same ticker can be a different company on another exchange or
// another provider's convention. Only an ISIN, which identifies a security,
// links them; a MIC on both sides tells whether it is also the same listing.
const (
	// LinkSameListing: same ISIN and same MIC. The same instrument from
	// different providers.
	LinkSameListing = "same_listing"
	// LinkSameSecurity: same ISIN, but the listings differ or are not known.
	// The same security; prices may be in other currencies or venues.
	LinkSameSecurity = "same_security"
	// LinkSameTicker: same ticker, nothing confirms or rules out that they are
	// the same instrument. Shown so the user can check; never merged.
	LinkSameTicker = "same_ticker"
	// LinkDifferent: same ticker but different ISINs. Different instruments.
	LinkDifferent = "different_instruments"
)

// Link is one group of related members.
type Link struct {
	Kind    string  `json:"kind"`
	ISIN    string  `json:"isin,omitempty"`
	Symbol  string  `json:"symbol,omitempty"`
	Members []int64 `json:"members"`
	Reason  string  `json:"reason"`
}

// identityOf is a member's ISIN and MIC: its own, otherwise its profile's.
func identityOf(m collections.Member, profiles map[Key]*Profile) (isin, mic string) {
	isin, mic = m.Instrument.ISIN, m.Instrument.MIC
	if p := profiles[KeyOf(m.Instrument)]; p != nil {
		if isin == "" {
			isin = p.ISIN
		}
		if mic == "" {
			mic = p.MIC
		}
	}
	return isin, mic
}

// baseTicker strips a provider's venue suffix so "VOLV-B.ST" and "VOLV-B"
// are compared as the same ticker. It is used only to flag possible matches.
func baseTicker(symbol string) string {
	if i := strings.IndexAny(symbol, ".:"); i > 0 {
		return symbol[:i]
	}
	return symbol
}

// Links finds the members of one collection that relate across providers.
func Links(members []collections.Member, profiles map[Key]*Profile) []Link {
	type entry struct {
		id        int64
		isin, mic string
		ticker    string
	}
	entries := make([]entry, 0, len(members))
	byISIN := map[string][]entry{}
	for _, m := range members {
		isin, mic := identityOf(m, profiles)
		e := entry{id: m.ID, isin: isin, mic: mic, ticker: baseTicker(m.Instrument.Symbol)}
		entries = append(entries, e)
		if isin != "" {
			byISIN[isin] = append(byISIN[isin], e)
		}
	}
	var out []Link
	for isin, group := range byISIN {
		if len(group) < 2 {
			continue
		}
		same := true
		for _, e := range group {
			same = same && e.mic != "" && e.mic == group[0].mic
		}
		l := Link{Kind: LinkSameSecurity, ISIN: isin, Reason: "Same ISIN; the listings differ or are not known, so prices may be on other venues or in other currencies."}
		if same {
			l.Kind, l.Reason = LinkSameListing, "Same ISIN and the same exchange (MIC): the same instrument from different providers."
		}
		for _, e := range group {
			l.Members = append(l.Members, e.id)
		}
		out = append(out, l)
	}
	byTicker := map[string][]entry{}
	for _, e := range entries {
		byTicker[e.ticker] = append(byTicker[e.ticker], e)
	}
	for ticker, group := range byTicker {
		if len(group) < 2 {
			continue
		}
		isins := map[string]bool{}
		unknown := false
		for _, e := range group {
			if e.isin == "" {
				unknown = true
			} else {
				isins[e.isin] = true
			}
		}
		var l Link
		switch {
		case len(isins) > 1:
			l = Link{Kind: LinkDifferent, Symbol: ticker, Reason: "Same ticker but different ISINs: these are different instruments."}
		case len(isins) == 1 && !unknown:
			continue // already linked by ISIN
		default:
			l = Link{Kind: LinkSameTicker, Symbol: ticker, Reason: "Same ticker, but no shared ISIN confirms that these are the same instrument. They are kept apart."}
		}
		for _, e := range group {
			l.Members = append(l.Members, e.id)
		}
		out = append(out, l)
	}
	for i := range out {
		sort.Slice(out[i].Members, func(a, b int) bool { return out[i].Members[a] < out[i].Members[b] })
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Kind != out[b].Kind {
			return out[a].Kind < out[b].Kind
		}
		return out[a].Members[0] < out[b].Members[0]
	})
	return out
}
