// Package sources records what an agent run retrieved and checks the final
// answer against it. The checks are mechanical: a passing sentence is
// "citation checked", not "true", and nothing here judges whether prose
// supports a claim.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"floweysales/internal/grounding"
	"floweysales/internal/sourcetrust"
)

// nowFunc is the clock of date validation; tests replace it.
var nowFunc = time.Now

// Access says how much of a source reached the model.
const (
	AccessListed = "listed" // a search hit: title and snippet only
	AccessRead   = "read"   // content reached the model
)

// Source classes. Only the first two can stand as primary evidence.
const (
	ClassPrimaryFiling = "primary_filing"
	ClassWebPage       = "web_page"
	ClassSocial        = "social_discussion" // opinion / public reaction, not proof of financial metrics
)

const maxExcerpt = 2000

// maxReadExcerpt bounds the page text kept for the audit. It is the text the
// model was given, so a figure in it can be checked, but it stays out of events.
const maxReadExcerpt = 16000

// Source is one thing a tool returned that an answer may cite.
type Source struct {
	ID         string `json:"id"` // S1, S2, ...
	Tool       string `json:"tool"`
	CallID     string `json:"call_id,omitempty"`
	Query      string `json:"query,omitempty"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	Class      string `json:"class"`
	Access     string `json:"access"`
	DocumentID string `json:"document_id,omitempty"` // SEC accession
	// Member names the team member who retrieved the source, when a
	// delegated team did; the parent run only saw what the member reported.
	Member      string `json:"member,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"` // distinct from the reporting period
	RetrievedAt string `json:"retrieved_at,omitempty"`
	// Excerpt is what the model received, bounded.
	Excerpt string `json:"excerpt,omitempty"`
	// SearchProvider names the web search provider that listed the source
	// (searxng, brave, tavily, google_grounding, ...). ResultType is
	// "grounded_source" for a page Gemini cited, which has no snippet of its own.
	SearchProvider string `json:"search_provider,omitempty"`
	ResultType     string `json:"result_type,omitempty"`
	// Trust is what is known beyond the text: source type and evidence role,
	// dates kept apart, reporting period, evidence state and why it ranked
	// where it did (see internal/sourcetrust).
	Trust *sourcetrust.Meta `json:"trust,omitempty"`
}

// Registry collects the sources of one run, in the order they were returned.
type Registry struct {
	list []Source
	byID map[string]int
}

func NewRegistry() *Registry { return &Registry{byID: map[string]int{}} }

// RegistryFrom rebuilds a registry from saved sources, for a task run that
// resumes: the ids the earlier rounds handed out (S1, S2, ...) keep their
// meaning and new sources continue the numbering. Entries whose ids are not
// S1..Sn in order are dropped with everything after them.
func RegistryFrom(saved []Source) *Registry {
	r := NewRegistry()
	for i, s := range saved {
		if s.ID != fmt.Sprintf("S%d", i+1) {
			break
		}
		r.byID[s.ID] = len(r.list)
		r.list = append(r.list, s)
	}
	return r
}

// Compact returns the sources with each excerpt cut to maxExcerpt bytes, at
// most max sources, for a record that only shows what was cited.
func Compact(list []Source, max, maxExcerpt int) []Source {
	if len(list) > max {
		list = list[:max]
	}
	out := make([]Source, len(list))
	for i, s := range list {
		if len(s.Excerpt) > maxExcerpt {
			s.Excerpt = strings.ToValidUTF8(s.Excerpt[:maxExcerpt], "")
		}
		out[i] = s
	}
	return out
}

// All returns the sources in registration order.
func (r *Registry) All() []Source { return append([]Source(nil), r.list...) }

func (r *Registry) Get(id string) (Source, bool) {
	i, ok := r.byID[id]
	if !ok {
		return Source{}, false
	}
	return r.list[i], true
}

func (r *Registry) add(s Source) Source {
	s.ID = fmt.Sprintf("S%d", len(r.list)+1)
	if len(s.Excerpt) > maxExcerpt {
		s.Excerpt = s.Excerpt[:maxExcerpt]
	}
	r.byID[s.ID] = len(r.list)
	r.list = append(r.list, s)
	return s
}

// sourceClassForURL identifies a narrowly defined set of social/community
// platforms. Unknown sites stay generic: a plausible-looking domain is not
// sufficient evidence of a regulator or issuer publication.
func sourceClassForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ClassWebPage
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	for _, domain := range []string{"reddit.com", "stocktwits.com", "twitter.com", "x.com", "threads.net", "bsky.app"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return ClassSocial
		}
	}
	return ClassWebPage
}

// Register reads a tool result and records the sources in it. It returns the
// result to give the model, with each source's id added so the model can cite
// it as [S3], and the sources it added. Tools that return no citable source
// pass through unchanged.
func (r *Registry) Register(tool, callID, argsJSON, result string) (string, []Source) {
	switch tool {
	case "search_web":
		return r.registerSearch(callID, argsJSON, result)
	case "get_sec_section":
		return r.registerSection(callID, result)
	case "read_source":
		return r.registerRead(argsJSON, result)
	case "delegate_to_team":
		return r.registerDelegation(callID, result)
	case "get_sec_filings":
		return r.registerFilings(callID, result)
	case "get_sec_facts":
		return r.registerFacts(callID, result)
	}
	return result, nil
}

// ReadTarget resolves the source_id of a read_source call to the URL of a
// search hit from this run. The model never supplies the address itself, so
// it cannot open a page no search returned.
func (r *Registry) ReadTarget(argsJSON string) (Source, error) {
	var args struct {
		SourceID string `json:"source_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.SourceID == "" {
		return Source{}, fmt.Errorf("source_id is required, for example S3")
	}
	src, ok := r.Get(strings.ToUpper(strings.TrimSpace(args.SourceID)))
	switch {
	case !ok:
		return Source{}, fmt.Errorf("%s is not a source returned in this run; use a source_id from a search_web result", args.SourceID)
	case src.Access == AccessRead:
		return Source{}, fmt.Errorf("%s has already been read; its content came with the earlier tool result", src.ID)
	case src.URL == "":
		return Source{}, fmt.Errorf("%s has no page address to open", src.ID)
	}
	return src, nil
}

// registerRead upgrades a search hit to a source the agent read: the page text
// the model received becomes the excerpt the audit checks figures against.
func (r *Registry) registerRead(argsJSON, result string) (string, []Source) {
	src, err := r.ReadTarget(argsJSON)
	if err != nil {
		return result, nil
	}
	var page struct {
		Title       string            `json:"title"`
		Text        string            `json:"text"`
		RetrievedAt string            `json:"retrieved_at"`
		ReadMeta    *sourcetrust.Meta `json:"read_meta"`
	}
	if json.Unmarshal([]byte(result), &page) != nil || strings.TrimSpace(page.Text) == "" {
		return result, nil
	}
	i := r.byID[src.ID]
	r.list[i].Access = AccessRead
	r.list[i].RetrievedAt = page.RetrievedAt
	if page.Title != "" {
		r.list[i].Title = page.Title
	}
	text := page.Text
	if len(text) > maxReadExcerpt {
		text = text[:maxReadExcerpt]
	}
	r.list[i].Excerpt = text
	r.list[i].mergeRead(page.ReadMeta)
	shown := r.list[i]
	if len(shown.Excerpt) > maxExcerpt {
		shown.Excerpt = shown.Excerpt[:maxExcerpt]
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil {
		return result, []Source{shown}
	}
	id, _ := json.Marshal(src.ID)
	doc["source_id"] = id
	out, err := json.Marshal(doc)
	if err != nil {
		return result, []Source{shown}
	}
	return string(out), []Source{shown}
}

// mergeRead folds what reading a page showed into the source's trust metadata
// and keeps the plain published_at in step with it.
func (s *Source) mergeRead(read *sourcetrust.Meta) {
	if s.Trust == nil {
		s.Trust = sourcetrust.ForSearchResult(s.URL, s.Title, "", s.PublishedAt, s.RetrievedAt, nowFunc())
	}
	if read != nil {
		s.Trust.MergeRead(read, nowFunc())
	} else {
		s.Trust.EvidenceState = sourcetrust.StatePageRead
	}
	s.PublishedAt = s.Trust.PublishedAt
}

// RecordReadFailure notes on the source that a read_source call for it failed,
// so it shows as a refused or unreadable lead and not as a read page. It
// returns the updated source for the event stream.
func (r *Registry) RecordReadFailure(argsJSON, reason string) (Source, bool) {
	var args struct {
		SourceID string `json:"source_id"`
	}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return Source{}, false
	}
	i, ok := r.byID[strings.ToUpper(strings.TrimSpace(args.SourceID))]
	if !ok || r.list[i].Access == AccessRead {
		return Source{}, false
	}
	s := &r.list[i]
	if s.Trust == nil {
		s.Trust = sourcetrust.ForSearchResult(s.URL, s.Title, "", s.PublishedAt, s.RetrievedAt, nowFunc())
	}
	s.Trust.MarkReadFailure(reason)
	shown := *s
	if len(shown.Excerpt) > maxExcerpt {
		shown.Excerpt = shown.Excerpt[:maxExcerpt]
	}
	return shown, true
}

func (r *Registry) registerSearch(callID, argsJSON, result string) (string, []Source) {
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil {
		return result, nil
	}
	var hits []map[string]any
	if json.Unmarshal(doc["results"], &hits) != nil || len(hits) == 0 {
		return result, nil
	}
	var args struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	var retrieved string
	_ = json.Unmarshal(doc["retrieved_at_utc"], &retrieved)
	var searchProvider string
	_ = json.Unmarshal(doc["provider"], &searchProvider)
	var added []Source
	for _, hit := range hits {
		link, _ := hit["url"].(string)
		title, _ := hit["title"].(string)
		snippet, _ := hit["snippet"].(string)
		resultType, _ := hit["type"].(string)
		published, _ := hit["published_at"].(string)
		// The search renderer classifies, dates and ranks each hit; a hit
		// without that (an older trace) is classified here with the same rules.
		modified, _ := hit["modified_at"].(string)
		if _, ok := parseDate(modified); !ok {
			modified = ""
		}
		var trust *sourcetrust.Meta
		if hit["trust"] != nil {
			if raw, err := json.Marshal(hit["trust"]); err == nil {
				var m sourcetrust.Meta
				if json.Unmarshal(raw, &m) == nil && m.Type != "" {
					trust = &m
				}
			}
		}
		if trust == nil {
			trust = sourcetrust.ForSearchResult(link, title, snippet, published, retrieved, nowFunc())
		}
		s := r.add(Source{Tool: "search_web", CallID: callID, Query: args.Query, URL: link, Title: title,
			Class: sourceClassForURL(link), Access: AccessListed, PublishedAt: trust.PublishedAt, ModifiedAt: modified, RetrievedAt: retrieved, Excerpt: title + "\n" + snippet,
			SearchProvider: searchProvider, ResultType: resultType, Trust: trust})
		hit["source_id"] = s.ID
		added = append(added, s)
	}
	raw, err := json.Marshal(hits)
	if err != nil {
		return result, added
	}
	doc["results"] = raw
	out, err := json.Marshal(doc)
	if err != nil {
		return result, added
	}
	return string(out), added
}

// maxMemberSources bounds the sources kept per team member.
const maxMemberSources = 12

// registerDelegation records the sources the members of a delegated team
// retrieved, so the parent's answer can cite them as [Sn] and the citation
// check treats them like its own: a member's search hit is a snippet, a page
// or filing the member opened is read, and the excerpt is the text the member
// received. Each source keeps the member's name. The model gets the ids in
// place of the member's raw source objects.
func (r *Registry) registerDelegation(callID, result string) (string, []Source) {
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil {
		return result, nil
	}
	var members []map[string]json.RawMessage
	if json.Unmarshal(doc["members"], &members) != nil || len(members) == 0 {
		return result, nil
	}
	var added []Source
	for _, m := range members {
		var name string
		_ = json.Unmarshal(m["name"], &name)
		var reported []Source
		if json.Unmarshal(m["sources"], &reported) != nil {
			delete(m, "sources")
			continue
		}
		seen := map[string]bool{}
		var shown []map[string]string
		for _, src := range reported {
			key := src.DocumentID
			if key == "" {
				key = normalizeURL(src.URL)
			}
			if (src.URL == "" && src.DocumentID == "") || seen[key] || len(shown) >= maxMemberSources {
				continue
			}
			seen[key] = true
			if !ValidClass(src.Class) {
				src.Class = sourceClassForURL(src.URL)
			}
			if src.Access != AccessRead {
				src.Access = AccessListed
			}
			s := r.add(Source{Tool: "delegate_to_team", CallID: callID, Member: name, Query: src.Query, URL: src.URL, Title: src.Title,
				Class: src.Class, Access: src.Access, DocumentID: src.DocumentID, PublishedAt: src.PublishedAt, ModifiedAt: src.ModifiedAt, RetrievedAt: src.RetrievedAt, Excerpt: src.Excerpt,
				SearchProvider: src.SearchProvider, ResultType: src.ResultType, Trust: src.Trust})
			added = append(added, s)
			shown = append(shown, map[string]string{"source_id": s.ID, "url": s.URL, "title": s.Title, "access": s.Access})
		}
		raw, err := json.Marshal(shown)
		if err != nil || len(shown) == 0 {
			delete(m, "sources")
			continue
		}
		m["sources"] = raw
	}
	raw, err := json.Marshal(members)
	if err != nil {
		return result, added
	}
	doc["members"] = raw
	out, err := json.Marshal(doc)
	if err != nil {
		return result, added
	}
	return string(out), added
}

func (r *Registry) registerSection(callID, result string) (string, []Source) {
	var sec struct {
		Form        string `json:"form"`
		Item        string `json:"item"`
		Accession   string `json:"accession"`
		Filed       string `json:"filed"`
		DocumentURL string `json:"document_url"`
		Excerpt     string `json:"excerpt"`
		RetrievedAt string `json:"retrieved_at"`
	}
	if json.Unmarshal([]byte(result), &sec) != nil || sec.DocumentURL == "" {
		return result, nil
	}
	s := r.add(Source{Tool: "get_sec_section", CallID: callID, URL: sec.DocumentURL, Title: strings.TrimSpace(sec.Form + " " + sec.Item),
		Class: ClassPrimaryFiling, Access: AccessRead, DocumentID: sec.Accession, PublishedAt: sec.Filed, RetrievedAt: sec.RetrievedAt, Excerpt: sec.Excerpt,
		Trust: sourcetrust.ForFiling(sec.Form, sec.Filed, sec.RetrievedAt, nowFunc())})
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil {
		return result, []Source{s}
	}
	id, _ := json.Marshal(s.ID)
	doc["source_id"] = id
	out, err := json.Marshal(doc)
	if err != nil {
		return result, []Source{s}
	}
	return string(out), []Source{s}
}

// Finding kinds.
const (
	Fabricated  = "fabricated"   // cites a source, or a URL, no tool returned
	SnippetOnly = "snippet_only" // cites a search hit the agent never read
	FigureNotIn = "figure_mismatch"
	Uncited     = "uncited"
	NotPrimary  = "not_primary"     // a secondary source cited where only primary filings count
	QuoteNotIn  = "quote_mismatch"  // a quoted passage that is not in the cited text
	Excluded    = "excluded_source" // a source of a type the user's settings do not accept
	Stale       = "stale"           // a source older than the recency limit, or with no date to check
	Unreachable = "unreachable"     // a cited link that no longer resolves
	// SentimentAsFact: a reported financial figure that rests only on community
	// discussion or opinion, which can show what people think, not what was reported.
	SentimentAsFact = "sentiment_as_fact"
)

// ValidClass reports whether class is a source type a setting may name.
func ValidClass(class string) bool {
	return class == ClassPrimaryFiling || class == ClassWebPage || class == ClassSocial
}

// Options tunes an audit.
type Options struct {
	// PrimaryOnly flags every citation of a source that is not a primary filing,
	// unless AllowClasses names the types that count instead.
	PrimaryOnly bool
	// AllowClasses, when set, are the only source types that count; a citation
	// of any other type is flagged. DenyClasses are never accepted and win over
	// AllowClasses.
	AllowClasses []string
	DenyClasses  []string
	// MaxAgeDays, when above zero, flags a cited source published longer ago
	// than that. A source with no publication date cannot be shown to be recent
	// and is flagged too.
	MaxAgeDays int
	// Now is the clock of the recency check; zero means the current time.
	Now time.Time
}

// classProblem says why a source type is not accepted, or "" when it is.
func (o Options) classProblem(class string) (kind, detail string) {
	for _, c := range o.DenyClasses {
		if c == class {
			return Excluded, "Sources of type " + class + " are excluded by the evidence settings."
		}
	}
	if len(o.AllowClasses) > 0 {
		for _, c := range o.AllowClasses {
			if c == class {
				return "", ""
			}
		}
		return Excluded, "Sources of type " + class + " are not among the types the evidence settings accept (" + strings.Join(o.AllowClasses, ", ") + ")."
	}
	if o.PrimaryOnly && class != ClassPrimaryFiling {
		return NotPrimary, "is not a primary filing, and this mode accepts primary filings only."
	}
	return "", ""
}

// ageProblem says why a source breaks the recency limit, or "" when it does not.
func (o Options) ageProblem(src Source) string {
	if o.MaxAgeDays <= 0 {
		return ""
	}
	published, ok := parseDate(src.PublishedAt)
	if !ok {
		return fmt.Sprintf("has no publication date, so it cannot be shown to be within the last %d days.", o.MaxAgeDays)
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	if age := int(now.Sub(published).Hours() / 24); age > o.MaxAgeDays {
		return fmt.Sprintf("was published %s, %d days ago; the limit is %d days.", published.UTC().Format("2006-01-02"), age, o.MaxAgeDays)
	}
	return ""
}

func parseDate(raw string) (time.Time, bool) { return sourcetrust.ParseDate(raw) }

// Finding is one problem the audit found, tied to the sentence it is in.
type Finding struct {
	Kind     string `json:"kind"`
	Sentence string `json:"sentence"`
	Source   string `json:"source,omitempty"`
	Detail   string `json:"detail"`
}

var (
	citation = regexp.MustCompile(`\[S(\d+)\]`)
	linkText = regexp.MustCompile(`https?://[^\s)\]>"']+`)
	// A sentence ends at . ! or ? followed by space or the end, or at a line break,
	// so decimals and URLs stay whole.
	sentenceEnd = regexp.MustCompile(`[.!?]+(?:\s|$)|\n`)
)

// Audit checks an answer against the registry. Sentences are checked one at
// a time:
//   - [Sn] that is not in the registry, or a URL no tool returned: fabricated
//   - a figure in a sentence citing only unread (listed) sources: snippet_only
//   - a figure that is not in the cited excerpts: figure_mismatch
//   - a figure that appears in a retrieved source, stated with no citation: uncited
//   - a quoted passage (five words or more) that is not in the cited text: quote_mismatch
//
// Only sentences with a figure (see grounding.Figures) are held to the
// figure rules, since prose cannot be checked mechanically.
func Audit(answer string, r *Registry) []Finding { return AuditWith(answer, r, Options{}) }

// AuditWith is Audit with options.
func AuditWith(answer string, r *Registry, opts Options) []Finding {
	var out []Finding
	known := map[string]bool{}
	var all []string
	for _, s := range r.list {
		all = append(all, s.Excerpt)
		if s.URL != "" {
			known[normalizeURL(s.URL)] = true
		}
	}
	for _, m := range splitSentences(answer) {
		text := strings.TrimSpace(m)
		if text == "" {
			continue
		}
		shown := shortSentence(text)
		cited := map[string]bool{}
		var excerpts []string
		var listedOnly = true
		var sentimentCited, factCapable int
		for _, c := range citation.FindAllStringSubmatch(text, -1) {
			id := "S" + c[1]
			if cited[id] {
				continue
			}
			cited[id] = true
			src, ok := r.Get(id)
			if !ok {
				out = append(out, Finding{Kind: Fabricated, Sentence: shown, Source: id, Detail: id + " is not a source any tool returned in this run."})
				continue
			}
			excerpts = append(excerpts, src.Excerpt)
			if src.Trust != nil && (src.Trust.Role == sourcetrust.RoleSentiment || src.Trust.Role == sourcetrust.RoleOpinion) {
				sentimentCited++
			} else {
				factCapable++
			}
			if kind, detail := opts.classProblem(src.Class); kind != "" {
				if kind == NotPrimary {
					detail = id + " " + detail
				}
				out = append(out, Finding{Kind: kind, Sentence: shown, Source: id, Detail: detail})
			}
			if detail := opts.ageProblem(src); detail != "" {
				out = append(out, Finding{Kind: Stale, Sentence: shown, Source: id, Detail: id + " " + detail})
			}
			if src.Access == AccessRead {
				listedOnly = false
			}
		}
		for _, link := range linkText.FindAllString(text, -1) {
			link = strings.TrimRight(link, ".,;:")
			if !known[normalizeURL(link)] {
				out = append(out, Finding{Kind: Fabricated, Sentence: shown, Source: link, Detail: "No tool returned this URL in this run."})
			}
		}
		if len(excerpts) > 0 {
			for _, q := range missingQuotes(citation.ReplaceAllString(text, ""), excerpts) {
				out = append(out, Finding{Kind: QuoteNotIn, Sentence: shown, Detail: "The quoted passage “" + q + "” is not in the cited text."})
			}
		}
		figures := grounding.Figures(citation.ReplaceAllString(text, ""))
		if len(figures) == 0 {
			continue
		}
		switch {
		case len(cited) == 0 && !strings.Contains(text, "http"):
			// Figures from other tools (prices, a DCF) need no marker. A figure
			// that appears in a retrieved source but is stated without one does.
			if len(all) > 0 && grounding.Check(citation.ReplaceAllString(text, ""), all...) == nil {
				out = append(out, Finding{Kind: Uncited, Sentence: shown, Detail: "States a figure that appears in a retrieved source without citing it."})
			}
		case len(excerpts) > 0:
			if sentimentCited > 0 && factCapable == 0 && sourcetrust.IsUnattributedFinancialClaim(text) {
				out = append(out, Finding{Kind: SentimentAsFact, Sentence: shown, Detail: "States a financial figure as fact, but every cited source is community discussion or opinion. Those show what people think, not reported figures; cite the filing or attribute the figure to the discussion."})
			}
			if listedOnly {
				out = append(out, Finding{Kind: SnippetOnly, Sentence: shown, Detail: "The cited source is a search result the agent did not open; only its title and snippet were seen."})
			}
			if err := grounding.Check(citation.ReplaceAllString(text, ""), excerpts...); err != nil {
				out = append(out, Finding{Kind: FigureNotIn, Sentence: shown, Detail: err.Error() + " (in the cited excerpt)"})
			}
		}
	}
	return out
}

// LinkChecker reports an error when a cited address does not resolve. The
// caller supplies it so that the address checks of the server apply and tests
// need no network.
type LinkChecker func(ctx context.Context, rawURL string) error

const (
	// MaxLinkChecks bounds how many distinct addresses one run checks; cited
	// links beyond it are left unchecked, not flagged.
	MaxLinkChecks    = 8
	linkCheckTimeout = 8 * time.Second
)

// LinkCache remembers what the link check found, so a correction round does
// not check the same address again, and counts the checks made.
type LinkCache struct {
	result  map[string]error
	checked int
}

// Findings checks the addresses of the sources the answer cites by marker and
// the agent never opened (an opened page was fetched, so it resolved). Each
// check is bounded by a timeout, and the run by MaxLinkChecks addresses.
func (c *LinkCache) Findings(ctx context.Context, answer string, r *Registry, check LinkChecker) []Finding {
	if check == nil {
		return nil
	}
	if c.result == nil {
		c.result = map[string]error{}
	}
	var out []Finding
	for _, m := range splitSentences(answer) {
		text := strings.TrimSpace(m)
		seen := map[string]bool{}
		for _, match := range citation.FindAllStringSubmatch(text, -1) {
			id := "S" + match[1]
			src, ok := r.Get(id)
			if seen[id] || !ok || src.Access == AccessRead || src.URL == "" {
				continue
			}
			seen[id] = true
			err, known := c.result[src.URL]
			if !known {
				if ctx.Err() != nil || c.checked >= MaxLinkChecks {
					continue
				}
				c.checked++
				cctx, cancel := context.WithTimeout(ctx, linkCheckTimeout)
				err = check(cctx, src.URL)
				cancel()
				if ctx.Err() != nil {
					continue // the run was cancelled; that says nothing about the link
				}
				c.result[src.URL] = err
			}
			if err != nil {
				msg := err.Error()
				if len(msg) > 160 {
					msg = msg[:160]
				}
				out = append(out, Finding{Kind: Unreachable, Sentence: shortSentence(text), Source: id, Detail: id + " (" + src.URL + ") did not resolve: " + msg})
			}
		}
	}
	return out
}

// shortSentence is how a finding shows its sentence.
func shortSentence(text string) string {
	if len(text) > 240 {
		return text[:240] + "…"
	}
	return text
}

// InsufficientEvidence is the answer when every sentence of a draft failed.
const InsufficientEvidence = "Insufficient verifiable evidence: every statement in the draft failed the citation check, so none is shown."

// Block removes the sentences the findings are about and returns what is left
// with a line saying what was removed, and the findings that led to it. The
// removed text is not repeated. When nothing is left the answer says that the
// evidence was insufficient.
func Block(answer string, found []Finding) (string, []Finding) {
	if len(found) == 0 {
		return answer, nil
	}
	flagged := map[string]bool{}
	for _, f := range found {
		flagged[f.Sentence] = true
	}
	var kept strings.Builder
	removed := 0
	kinds := map[string]bool{}
	var order []string
	for _, m := range splitSentences(answer) {
		text := strings.TrimSpace(m)
		if text != "" && flagged[shortSentence(text)] {
			removed++
			continue
		}
		kept.WriteString(m)
	}
	for _, f := range found {
		if !kinds[f.Kind] {
			kinds[f.Kind] = true
			order = append(order, f.Kind)
		}
	}
	if removed == 0 {
		return answer, nil
	}
	body := strings.TrimSpace(kept.String())
	if body == "" {
		return InsufficientEvidence + " Checks that failed: " + strings.Join(order, ", ") + ".", found
	}
	return body + fmt.Sprintf("\n\nRemoved %d statement(s) that failed the citation check (%s). Sources lists the findings.", removed, strings.Join(order, ", ")), found
}

func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.ToLower(raw)
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func splitSentences(text string) []string {
	var out []string
	start := 0
	for _, loc := range sentenceEnd.FindAllStringIndex(text, -1) {
		out = append(out, text[start:loc[1]])
		start = loc[1]
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

const minQuoteWords = 5

var (
	quoted    = regexp.MustCompile(`"([^"]{10,600})"|“([^”]{10,600})”`)
	quoteGaps = regexp.MustCompile(`\s*(?:\[\s*(?:\.{3}|…)\s*\]|\.{3}|…)\s*`)
	dashes    = strings.NewReplacer("\u2013", "-", "\u2014", "-", "\u2018", "'", "\u2019", "'", "\u201c", `"`, "\u201d", `"`, "\u00a0", " ")
)

// foldText lower-cases and collapses whitespace so a quote that differs from
// the source only in case, spacing or typographic punctuation still matches.
func foldText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(dashes.Replace(s))), " ")
}

// missingQuotes returns the quoted passages of sentence (five words or more)
// that none of the excerpts contain. A quote with an ellipsis is split there,
// and each part of five words or more must be found. Short quotes are skipped:
// they are names and terms, not claims about what a source said.
func missingQuotes(sentence string, excerpts []string) []string {
	var folded []string
	for _, e := range excerpts {
		folded = append(folded, foldText(e))
	}
	var missing []string
	for _, m := range quoted.FindAllStringSubmatch(sentence, -1) {
		q := m[1] + m[2]
		if len(strings.Fields(q)) < minQuoteWords {
			continue
		}
		found := true
		for _, part := range quoteGaps.Split(q, -1) {
			part = strings.Trim(foldText(part), " .,;:")
			if len(strings.Fields(part)) < minQuoteWords {
				continue
			}
			in := false
			for _, e := range folded {
				if strings.Contains(e, part) {
					in = true
					break
				}
			}
			if !in {
				found = false
				break
			}
		}
		if !found {
			missing = append(missing, strings.TrimSpace(q))
		}
	}
	return missing
}
