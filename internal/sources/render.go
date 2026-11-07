package sources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// markerPattern matches a citation marker and the space before it.
var markerPattern = regexp.MustCompile(`[ \t]*\[S(\d+)\]`)

// unlinkedMarker is a marker that is not already a Markdown link text or part
// of one: [S1] not followed by "(" and not wrapped in a second bracket.
var unlinkedMarker = regexp.MustCompile(`\[S(\d+)\]`)

// maxFooterSources bounds the sources listed under a rendered answer.
const maxFooterSources = 30

var credentialParam = regexp.MustCompile(`(?i)(key|token|secret|sig|signature|auth|session|passw|credential)`)

// SafeLink returns the address when it is an http(s) link that is safe to put
// in Markdown, else "". Credentials in the address and fragments are dropped
// (an address with credentials is refused), the value of a parameter that
// looks like a credential becomes REDACTED, and brackets and parentheses are
// escaped so the link cannot end early.
func SafeLink(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	u.Fragment = ""
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if credentialParam.MatchString(k) {
				q.Set(k, "REDACTED")
			}
		}
		u.RawQuery = q.Encode()
	}
	out := u.String()
	if strings.ContainsAny(out, " \t\r\n<>\"") {
		return ""
	}
	return strings.NewReplacer("(", "%28", ")", "%29", "[", "%5B", "]", "%5D").Replace(out)
}

func mdText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.NewReplacer("[", "(", "]", ")", "|", "/").Replace(s)
}

// RenderCitations turns the [Sn] markers of a Markdown answer into links to
// the source and appends a Sources list for the markers that resolve, from the
// sources the run persisted. Each entry carries what is known about the
// document: title, tool, filing accession, publication and retrieval time
// (UTC) and whether the agent read the page or only saw a search result. A
// marker with no source in this run is left as written and listed as
// unresolved, so a reader never mistakes it for a citation. The answer is
// returned unchanged when it has no markers.
func RenderCitations(answer string, list []Source) string {
	found := unlinkedMarker.FindAllStringSubmatchIndex(answer, -1)
	if len(found) == 0 {
		return answer
	}
	byID := map[string]Source{}
	for _, s := range list {
		byID[s.ID] = s
	}
	cited := map[string]bool{}
	var b strings.Builder
	last := 0
	for _, m := range found {
		id := "S" + answer[m[2]:m[3]]
		cited[id] = true
		b.WriteString(answer[last:m[0]])
		last = m[1]
		marker := answer[m[0]:m[1]]
		// Already a link ([S1](...)) or a link label ([[S1]](...)): leave it.
		if strings.HasPrefix(answer[m[1]:], "(") || (m[0] > 0 && answer[m[0]-1] == '[' && strings.HasPrefix(answer[m[1]:], "]")) {
			b.WriteString(marker)
			continue
		}
		if link := SafeLink(byID[id].URL); link != "" {
			b.WriteString("[" + marker + "](" + link + ")")
		} else {
			b.WriteString(marker)
		}
	}
	b.WriteString(answer[last:])
	ids := make([]string, 0, len(cited))
	for id := range cited {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return markerNumber(ids[i]) < markerNumber(ids[j]) })
	b.WriteString("\n\n**Sources**\n")
	for i, id := range ids {
		if i == maxFooterSources {
			fmt.Fprintf(&b, "- … %d more cited source(s) not listed\n", len(ids)-i)
			break
		}
		s, ok := byID[id]
		if !ok {
			fmt.Fprintf(&b, "- [%s] not a source retrieved in this run (unresolved citation)\n", id)
			continue
		}
		b.WriteString("- " + SourceLine(s) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func markerNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "S"))
	return n
}

// SourceLine is the one-line description of a source used under a rendered
// answer: [S3] Title, address, document id, dates, and how much was read.
func SourceLine(s Source) string {
	title := mdText(s.Title)
	if title == "" {
		title = mdText(s.URL)
	}
	parts := []string{"[" + s.ID + "] " + title}
	if link := SafeLink(s.URL); link != "" {
		parts = append(parts, "<"+link+">")
	}
	if s.DocumentID != "" {
		parts = append(parts, "accession "+mdText(s.DocumentID))
	}
	if s.PublishedAt != "" {
		parts = append(parts, "published "+mdText(s.PublishedAt))
	} else {
		parts = append(parts, "publication date unknown")
	}
	if s.RetrievedAt != "" {
		parts = append(parts, "retrieved "+mdText(s.RetrievedAt)+" UTC")
	}
	access := "search result only (the page was not opened)"
	if s.Access == AccessRead {
		access = "read by the agent"
	}
	parts = append(parts, access)
	if s.Member != "" {
		parts = append(parts, "found by "+mdText(s.Member))
	}
	return strings.Join(parts, "; ")
}

// JSONCitation ties the sources an agent cited to one string of a JSON
// output, so the output keeps exactly the declared structure and the
// provenance travels beside it. Path is a JSON Pointer (RFC 6901).
type JSONCitation struct {
	Path    string   `json:"path"`
	Sources []string `json:"sources"`
	// Text is the string the markers were removed from, bounded.
	Text string `json:"text,omitempty"`
}

const maxJSONCitations = 200

// ExtractJSONCitations removes [Sn] markers from the string values of a JSON
// document and reports where each was. The returned document is the input
// when no marker was found; otherwise it is re-encoded (object keys sorted,
// numbers kept as written). Only string values are touched: keys, numbers and
// structure are not. It returns an error only for input that is not JSON.
func ExtractJSONCitations(raw string) (string, []JSONCitation, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return raw, nil, err
	}
	var cites []JSONCitation
	changed := false
	var walk func(v any, path string) any
	walk = func(v any, path string) any {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				t[k] = walk(child, path+"/"+pointerEscape(k))
			}
			return t
		case []any:
			for i, child := range t {
				t[i] = walk(child, path+"/"+strconv.Itoa(i))
			}
			return t
		case string:
			ms := markerPattern.FindAllStringSubmatch(t, -1)
			if len(ms) == 0 {
				return t
			}
			changed = true
			var ids []string
			seen := map[string]bool{}
			for _, m := range ms {
				if id := "S" + m[1]; !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			cleaned := strings.TrimSpace(markerPattern.ReplaceAllString(t, ""))
			if len(cites) < maxJSONCitations {
				text := cleaned
				if len(text) > 300 {
					text = strings.ToValidUTF8(text[:300], "") + "…"
				}
				cites = append(cites, JSONCitation{Path: path, Sources: ids, Text: text})
			}
			return cleaned
		}
		return v
	}
	doc = walk(doc, "")
	if !changed {
		return raw, nil, nil
	}
	sort.SliceStable(cites, func(i, j int) bool { return cites[i].Path < cites[j].Path })
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return raw, nil, err
	}
	return strings.TrimSpace(out.String()), cites, nil
}

func pointerEscape(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

// JSONProse returns the string values of a JSON document as text, one per
// line and ended with a period, so the citation check can read them like the
// sentences of a prose answer. Text that is not JSON is returned unchanged.
func JSONProse(raw string) string {
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return raw
	}
	var lines []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k])
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		case string:
			s := strings.TrimSpace(strings.ReplaceAll(t, "\n", " "))
			if s == "" {
				return
			}
			// A marker after the final period still belongs to the sentence.
			core := strings.TrimSpace(markerPattern.ReplaceAllString(s, ""))
			if !strings.HasSuffix(core, ".") && !strings.HasSuffix(core, "!") && !strings.HasSuffix(core, "?") {
				s += "."
			}
			lines = append(lines, s)
		}
	}
	walk(doc)
	return strings.Join(lines, "\n")
}
