package instruments

import (
	"fmt"
	"strings"
	"unicode"
)

// News relevance for a collection. A provider lists a headline under one of
// the collection's instruments; the text then says how it relates to the
// collection. Matching is literal text matching, stated as a reason, never a
// judgment about the story.
const (
	// RelevanceDirect: the headline or summary names a member by its name or symbol.
	RelevanceDirect = "direct"
	// RelevanceThematic: it names none, but mentions a theme or industry of a member.
	RelevanceThematic = "thematic"
	// RelevanceListed: the provider listed it under a member, and the text names
	// neither the member nor its themes.
	RelevanceListed = "listed"
)

// NewsSubject is one collection member as the news matcher sees it.
type NewsSubject struct {
	MemberID int64
	Key      Key
	Name     string   // the member's or its profile's name; empty when unknown
	Industry string   // effective industry text; empty when unknown
	Themes   []string // the user's themes
}

// NewsMatch is one reason a headline relates to the collection.
type NewsMatch struct {
	Relevance string  `json:"relevance"`
	Members   []int64 `json:"members"`
	Term      string  `json:"term,omitempty"`
	Reason    string  `json:"reason"`
}

// RelateNews says how a headline relates to the collection. listedBy names
// the members the provider listed it under; subjects are all members. The
// overall relevance is the strongest match.
func RelateNews(title, summary, provider string, listedBy []int64, subjects []NewsSubject) (string, []NewsMatch) {
	text := title + "\n" + summary
	var direct, thematic []NewsMatch
	named := map[int64]bool{}
	byID := map[int64]NewsSubject{}
	for _, s := range subjects {
		byID[s.MemberID] = s
		if term, ok := namesSubject(text, s); ok {
			named[s.MemberID] = true
			direct = append(direct, NewsMatch{Relevance: RelevanceDirect, Members: []int64{s.MemberID}, Term: term,
				Reason: fmt.Sprintf("Names %s (%s %s)", quoted(term), s.Key.Provider, s.Key.Symbol)})
		}
	}
	// One thematic match per term, naming every member that carries it.
	type themed struct {
		kind    string
		term    string
		members []int64
	}
	var terms []*themed
	index := map[string]*themed{}
	add := func(kind, term string, id int64) {
		k := kind + "\x00" + strings.ToLower(term)
		if t, ok := index[k]; ok {
			if !containsID(t.members, id) {
				t.members = append(t.members, id)
			}
			return
		}
		t := &themed{kind: kind, term: term, members: []int64{id}}
		index[k] = t
		terms = append(terms, t)
	}
	for _, s := range subjects {
		for _, theme := range s.Themes {
			if mentionsTerm(text, theme) {
				add("theme", theme, s.MemberID)
			}
		}
		if s.Industry != "" && mentionsTerm(text, s.Industry) {
			add("industry", s.Industry, s.MemberID)
		}
	}
	for _, t := range terms {
		thematic = append(thematic, NewsMatch{Relevance: RelevanceThematic, Members: t.members, Term: t.term,
			Reason: fmt.Sprintf("Mentions %s %s", t.kind, quoted(t.term))})
	}
	var listed []NewsMatch
	for _, id := range listedBy {
		s, ok := byID[id]
		if !ok || named[id] {
			continue
		}
		listed = append(listed, NewsMatch{Relevance: RelevanceListed, Members: []int64{id},
			Reason: fmt.Sprintf("Listed by %s under %s", provider, s.Key.Symbol)})
	}
	matches := append(append(direct, thematic...), listed...)
	switch {
	case len(direct) > 0:
		return RelevanceDirect, matches
	case len(thematic) > 0:
		return RelevanceThematic, matches
	default:
		return RelevanceListed, matches
	}
}

// namesSubject reports whether text names the member: its symbol, written in
// capitals and at least three characters long, or its name without legal
// suffixes such as "Inc." or "AB".
func namesSubject(text string, s NewsSubject) (string, bool) {
	if sym := symbolTerm(s.Key.Symbol); sym != "" && containsWord(text, sym, true) {
		return sym, true
	}
	if name := shortName(s.Name); name != "" && containsWord(text, name, false) {
		return name, true
	}
	return "", false
}

// symbolTerm is the part of a symbol a headline would print: "VOLV-B.ST" is
// written "VOLV-B". Short or lower-case symbols are not matched: "AI" or
// "bitcoin" as a symbol would match ordinary words.
func symbolTerm(symbol string) string {
	s := strings.TrimLeft(strings.TrimSpace(symbol), "^$")
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	if len(s) < 3 || s != strings.ToUpper(s) || strings.IndexFunc(s, unicode.IsLetter) < 0 || strings.ContainsAny(s, "=/ ") {
		return ""
	}
	return s
}

// legalSuffixes are dropped from the end of a name before matching.
var legalSuffixes = map[string]bool{
	"inc": true, "incorporated": true, "corp": true, "corporation": true, "co": true, "company": true,
	"ltd": true, "limited": true, "plc": true, "ab": true, "publ": true, "sa": true, "ag": true, "nv": true,
	"se": true, "asa": true, "oyj": true, "as": true, "spa": true, "llc": true, "lp": true,
	"holdings": true, "holding": true, "group": true, "adr": true, "ord": true, "shares": true,
}

// shortName strips legal suffixes and share classes: "NVIDIA Corporation" is
// "NVIDIA", "AB Volvo (publ) ser. B" is "Volvo". Names shorter than three letters are not matched.
func shortName(name string) string {
	words := strings.Fields(name)
	norm := func(w string) string { return strings.ToLower(strings.Trim(w, ".,()-&")) }
trim:
	for len(words) > 1 {
		last := norm(words[len(words)-1])
		prev := norm(words[len(words)-2])
		switch {
		case last == "" || legalSuffixes[last]:
			words = words[:len(words)-1]
		case len(last) == 1 && (prev == "class" || prev == "ser" || prev == "series"):
			words = words[:len(words)-2]
		default:
			break trim
		}
	}
	for len(words) > 1 && (strings.EqualFold(words[0], "the") || strings.EqualFold(words[0], "AB")) {
		words = words[1:]
	}
	out := strings.Trim(strings.Join(words, " "), " ,.-")
	if len([]rune(out)) < 3 {
		return ""
	}
	return out
}

// mentionsTerm matches a theme or industry. Terms of up to three characters,
// such as "AI" or "EV", match only as written, so "ai" in ordinary text does not.
func mentionsTerm(text, term string) bool {
	term = strings.TrimSpace(term)
	if len([]rune(term)) < 2 {
		return false
	}
	return containsWord(text, term, len([]rune(term)) <= 3)
}

// containsWord finds phrase in text at word boundaries.
func containsWord(text, phrase string, caseSensitive bool) bool {
	if !caseSensitive {
		text, phrase = strings.ToLower(text), strings.ToLower(phrase)
	}
	for from := 0; ; {
		i := strings.Index(text[from:], phrase)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(phrase)
		if boundaryBefore(text, start) && boundaryAfter(text, end) {
			return true
		}
		from = start + 1
	}
}

func boundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	r := lastRune(s[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r := []rune(s[i:])[0]
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func lastRune(s string) rune {
	r := []rune(s)
	return r[len(r)-1]
}

func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func quoted(s string) string { return `"` + s + `"` }
