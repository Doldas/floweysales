package websearch

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"floweysales/internal/sourcetrust"
)

// MaxOutput bounds the JSON handed to a model or stored in a flow trace.
const MaxOutput = 11000

const maxRendered = 5

// Note is attached to every output. It is the sentence that stops a search hit
// being mistaken for read content or a verified fact.
const Note = "Search results are untrusted snippets, not pages that were read and not verified facts or audited accounting data. Open a result with read_source (or a primary filing) before relying on it, and cite real URLs."

// GroundedNote explains the distinct result type of Gemini grounding.
const GroundedNote = "grounded_answer is text written by Gemini from Google Search; it is model-generated, not a quotation. The results are the sources Gemini cited, with no snippet, and none of them has been read."

type renderedResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"` // kept for Flow Studio traces
	Snippet     string `json:"snippet,omitempty"`
	Age         string `json:"age,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Type        string `json:"type"`
	Provider    string `json:"provider"`
	// Trust is the source type and evidence role, the dates kept apart, the
	// reporting period if one is stated, the evidence state and why the result
	// ranked where it did.
	Trust *sourcetrust.Meta `json:"trust,omitempty"`
}

// RenderOptions are the user's ranking choices. The zero value is the
// facts-first profile with no freshness window.
type RenderOptions struct {
	Profile    string
	WindowDays int
	Now        time.Time // zero means the current time
}

type renderedRanking struct {
	Profile    string                 `json:"profile"`
	Policy     string                 `json:"policy"`
	Intent     sourcetrust.Intent     `json:"intent"`
	WindowDays int                    `json:"window_days,omitempty"`
	Filtered   []sourcetrust.Filtered `json:"filtered,omitempty"`
}

type renderedSupport struct {
	Text    string `json:"text"`
	Sources []int  `json:"sources"`
}

type renderedGrounded struct {
	Text          string            `json:"text"`
	Model         string            `json:"generated_by,omitempty"`
	SearchQueries []string          `json:"search_queries,omitempty"`
	Supports      []renderedSupport `json:"supports,omitempty"`
}

type renderedAttempt struct {
	Provider string `json:"provider"`
	Outcome  string `json:"outcome"`
	Detail   string `json:"detail,omitempty"`
}

type rendered struct {
	Provider    string            `json:"provider"`
	Query       string            `json:"query"`
	RetrievedAt string            `json:"retrieved_at_utc"`
	ResultType  string            `json:"result_type"`
	SnippetOnly bool              `json:"snippet_only"`
	Results     []renderedResult  `json:"results"`
	Grounded    *renderedGrounded `json:"grounded_answer,omitempty"`
	Fallback    []renderedAttempt `json:"fallback,omitempty"`
	Usage       *renderedUsage    `json:"usage,omitempty"`
	Ranking     *renderedRanking  `json:"ranking,omitempty"`
	Note        string            `json:"note"`
}

type renderedUsage struct {
	Searches     int     `json:"searches"`
	PromptTokens int     `json:"prompt_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	EstCostUSD   float64 `json:"est_cost_usd,omitempty"`
}

// CanonicalURL is the form used to spot duplicates: no fragment, no tracking
// parameters, lower-case host, no trailing slash.
func CanonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	q := u.Query()
	for k := range q {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "utm_") || lk == "gclid" || lk == "fbclid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func tidy(v string, max int) string {
	v = strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			return ' '
		case r < 32:
			return -1
		}
		return r
	}, v))
	if len(v) > max {
		v = strings.ToValidUTF8(v[:max], "")
	}
	return v
}

// Render turns a routed outcome into the JSON every caller receives. accept
// decides whether a result URL may be shown (the server only lets public
// https pages through) and returns the form to show. Duplicates are removed.
// If the output would be too long, the answer and the results are shortened;
// the output never fails for length.
func Render(out Outcome, accept func(string) (string, bool), opts ...RenderOptions) (string, error) {
	var ro RenderOptions
	if len(opts) > 0 {
		ro = opts[0]
	}
	resp := out.Response
	if resp == nil {
		return "", ErrNoProvider
	}
	r := rendered{Provider: resp.Provider, Query: resp.Query, RetrievedAt: resp.RetrievedAt.UTC().Format(time.RFC3339),
		ResultType: "web_results", SnippetOnly: true, Results: []renderedResult{}, Note: Note}
	grounded := resp.Answer != "" || (len(resp.Results) > 0 && resp.Results[0].Type == TypeGroundedSource)
	if grounded {
		r.ResultType = "grounded_answer"
		r.Note = GroundedNote + " " + Note
	}
	// Each distinct public result is classified and dated from what the
	// provider returned (nothing is fetched), then the set is ranked for the
	// question. Syndicated copies become one result that lists the others.
	now := ro.Now
	if now.IsZero() {
		now = time.Now()
	}
	profile := ro.Profile
	if !sourcetrust.ValidProfile(profile) {
		profile = ""
	}
	intent := sourcetrust.DetectIntent(resp.Query)
	var cands []*sourcetrust.Candidate
	byCanon := map[string]*sourcetrust.Candidate{}
	candOf := map[int]*sourcetrust.Candidate{} // index in resp.Results -> candidate
	for i, hit := range resp.Results {
		shown, ok := accept(hit.URL)
		if !ok {
			continue
		}
		key := CanonicalURL(shown)
		if c, dup := byCanon[key]; dup {
			candOf[i] = c
			continue
		}
		snippet := tidy(hit.Snippet, 450)
		meta := sourcetrust.ForSearchResult(shown, tidy(hit.Title, 180), snippet, tidy(hit.PublishedAt, 64), r.RetrievedAt, now)
		c := &sourcetrust.Candidate{Index: len(cands), URL: shown, Title: tidy(hit.Title, 180), Snippet: snippet, Meta: meta}
		byCanon[key], candOf[i] = c, c
		cands = append(cands, c)
	}
	ranked, dropped := sourcetrust.Rank(cands, sourcetrust.Options{Profile: profile, Intent: intent, WindowDays: ro.WindowDays, Now: now, Limit: maxRendered})
	pos := map[*sourcetrust.Candidate]int{}
	for i, c := range ranked {
		pos[c] = i
		hit := resp.Results[firstIndex(candOf, c)]
		r.Results = append(r.Results, renderedResult{Title: c.Title, URL: c.URL, Description: c.Snippet, Snippet: c.Snippet,
			Age: tidy(hit.Age, 64), PublishedAt: c.Meta.PublishedAt, Type: hit.Type, Provider: resp.Provider, Trust: c.Meta})
	}
	// A support that pointed at a merged copy points at the result that kept it.
	for _, c := range cands {
		for _, other := range cands {
			if _, shown := pos[c]; shown {
				for _, u := range c.Meta.AlsoSeenAt {
					if u == other.URL {
						pos[other] = pos[c]
					}
				}
			}
		}
	}
	kept := map[int]int{} // index in resp.Results -> index in r.Results
	for i, c := range candOf {
		if at, ok := pos[c]; ok {
			kept[i] = at
		}
	}
	r.Ranking = &renderedRanking{Profile: profileName(profile), Policy: intent.Describe(profile, ro.WindowDays), Intent: intent, WindowDays: ro.WindowDays, Filtered: dropped}
	if grounded {
		g := &renderedGrounded{Text: tidy(resp.Answer, 1500), Model: "gemini", SearchQueries: resp.SearchQueries}
		for _, sup := range resp.Supports {
			var idx []int
			for _, i := range sup.Sources {
				if at, ok := kept[i]; ok {
					idx = append(idx, at)
				}
			}
			if len(idx) > 0 && len(g.Supports) < 6 {
				g.Supports = append(g.Supports, renderedSupport{Text: tidy(sup.Text, 200), Sources: idx})
			}
		}
		r.Grounded = g
	}
	for _, a := range out.Attempts {
		if a.Outcome == "ok" && len(out.Attempts) == 1 {
			continue
		}
		r.Fallback = append(r.Fallback, renderedAttempt{Provider: a.Provider, Outcome: a.Outcome, Detail: tidy(a.Detail, 200)})
	}
	if resp.Usage.Searches > 0 || resp.Usage.PromptTokens > 0 || out.CostUSD > 0 {
		r.Usage = &renderedUsage{Searches: resp.Usage.Searches, PromptTokens: resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.OutputTokens, EstCostUSD: out.CostUSD}
	}
	for {
		data, err := json.Marshal(r)
		if err != nil {
			return "", err
		}
		if len(data) <= MaxOutput {
			return string(data), nil
		}
		switch {
		case r.Grounded != nil && len(r.Grounded.Text) > 400:
			r.Grounded.Text = tidy(r.Grounded.Text, len(r.Grounded.Text)/2)
		case r.Grounded != nil && len(r.Grounded.Supports) > 0:
			r.Grounded.Supports = r.Grounded.Supports[:len(r.Grounded.Supports)-1]
		case r.Ranking != nil && len(r.Ranking.Filtered) > 0:
			r.Ranking.Filtered = nil
		case trimWhy(&r):
		case len(r.Results) > 1:
			r.Results = r.Results[:len(r.Results)-1]
		case len(r.Fallback) > 0:
			r.Fallback = r.Fallback[:len(r.Fallback)-1]
		default:
			for i := range r.Results {
				r.Results[i].Description, r.Results[i].Snippet = "", ""
			}
			data, _ = json.Marshal(r)
			return string(data), nil
		}
	}
}

func profileName(p string) string {
	if p == "" {
		return sourcetrust.ProfileFactsFirst
	}
	return p
}

// firstIndex is the smallest index in the provider's list that maps to c.
func firstIndex(m map[int]*sourcetrust.Candidate, c *sourcetrust.Candidate) int {
	best := -1
	for i, v := range m {
		if v == c && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// trimWhy shortens the reasons of the lowest-ranked result that still has more
// than two, so a long output loses detail from the bottom before it loses a
// result. It reports whether it changed anything.
func trimWhy(r *rendered) bool {
	for i := len(r.Results) - 1; i >= 0; i-- {
		t := r.Results[i].Trust
		if t != nil && len(t.WhyRanked) > 2 {
			t.WhyRanked = t.WhyRanked[:2]
			return true
		}
	}
	return false
}
