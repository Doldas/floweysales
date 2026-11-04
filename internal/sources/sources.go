// Package sources records what an agent run retrieved and checks the final
// answer against it. The checks are mechanical: a passing sentence is
// "citation checked", not "true", and nothing here judges whether prose
// supports a claim.
package sources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"floweysales/internal/grounding"
)

// Access says how much of a source reached the model.
const (
	AccessListed = "listed" // a search hit: title and snippet only
	AccessRead   = "read"   // content reached the model
)

// Source classes. Only the first two can stand as primary evidence.
const (
	ClassPrimaryFiling = "primary_filing"
	ClassWebPage       = "web_page"
)

const maxExcerpt = 2000

// maxReadExcerpt bounds the page text kept for the audit. It is the text the
// model was given, so a figure in it can be checked, but it stays out of events.
const maxReadExcerpt = 16000

// Source is one thing a tool returned that an answer may cite.
type Source struct {
	ID          string `json:"id"` // S1, S2, ...
	Tool        string `json:"tool"`
	CallID      string `json:"call_id,omitempty"`
	Query       string `json:"query,omitempty"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	Class       string `json:"class"`
	Access      string `json:"access"`
	DocumentID  string `json:"document_id,omitempty"` // SEC accession
	PublishedAt string `json:"published_at,omitempty"`
	RetrievedAt string `json:"retrieved_at,omitempty"`
	// Excerpt is what the model received, bounded.
	Excerpt string `json:"excerpt,omitempty"`
}

// Registry collects the sources of one run, in the order they were returned.
type Registry struct {
	list []Source
	byID map[string]int
}

func NewRegistry() *Registry { return &Registry{byID: map[string]int{}} }

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
		Title       string `json:"title"`
		Text        string `json:"text"`
		RetrievedAt string `json:"retrieved_at"`
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
	var added []Source
	for _, hit := range hits {
		link, _ := hit["url"].(string)
		title, _ := hit["title"].(string)
		snippet, _ := hit["snippet"].(string)
		s := r.add(Source{Tool: "search_web", CallID: callID, Query: args.Query, URL: link, Title: title,
			Class: ClassWebPage, Access: AccessListed, RetrievedAt: retrieved, Excerpt: title + "\n" + snippet})
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
		Class: ClassPrimaryFiling, Access: AccessRead, DocumentID: sec.Accession, PublishedAt: sec.Filed, RetrievedAt: sec.RetrievedAt, Excerpt: sec.Excerpt})
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
	NotPrimary  = "not_primary" // a secondary source cited where only primary filings count
)

// Options tunes an audit.
type Options struct {
	// PrimaryOnly flags every citation of a source that is not a primary filing.
	PrimaryOnly bool
}

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
		shown := text
		if len(shown) > 240 {
			shown = shown[:240] + "…"
		}
		cited := map[string]bool{}
		var excerpts []string
		var listedOnly = true
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
			if opts.PrimaryOnly && src.Class != ClassPrimaryFiling {
				out = append(out, Finding{Kind: NotPrimary, Sentence: shown, Source: id, Detail: id + " is not a primary filing, and this mode accepts primary filings only."})
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
