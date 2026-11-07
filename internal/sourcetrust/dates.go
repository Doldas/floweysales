package sourcetrust

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Evidence states say how much of a source the agent has. They are shown
// differently everywhere: a snippet, a blocked page and vision-derived text
// are never the same thing.
const (
	StateSnippetOnly   = "snippet_only"   // a search hit: title and snippet
	StatePageRead      = "page_read"      // an HTML or text page whose content reached the model
	StatePDFText       = "pdf_text"       // text extracted from a PDF's text layer
	StateVisionDerived = "vision_derived" // part of the content was transcribed by a vision model
	StateAccessDenied  = "access_denied"  // the site refused the request (HTTP 403 or 429); nothing was read
	StateReadFailed    = "read_failed"    // the page could not be read for another reason
)

// Date confidence levels.
const (
	ConfHigh   = "high"
	ConfMedium = "medium"
	ConfLow    = "low"
	ConfNone   = "none"
)

// Date sources.
const (
	DateSearchProvider = "search_provider"
	DateJSONLD         = "json_ld"
	DateMeta           = "meta_tag"
	DateTimeElement    = "time_element"
	DateFilingIndex    = "filing_index"
	DatePDFCover       = "pdf_cover"
	DateUnknown        = "unknown"
)

// Meta is what is known about a source beyond its text. Every date is kept in
// its own field and in the form the source gave it (time zone and precision
// included); comparisons are done on UTC. A date that is not known is empty,
// never today.
type Meta struct {
	Type      string `json:"source_type"`
	Role      string `json:"evidence_role"`
	TypeBasis string `json:"type_basis,omitempty"`

	PublishedAt string `json:"published_at,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	// EffectiveAt is the date the data is as of, when the source states one.
	EffectiveAt string `json:"effective_at,omitempty"`
	RetrievedAt string `json:"retrieved_at,omitempty"`
	// File dates: weak hints about the file, never the reporting period.
	FileCreatedAt    string `json:"file_created_at,omitempty"`
	FileModifiedAt   string `json:"file_modified_at,omitempty"`
	HTTPLastModified string `json:"http_last_modified,omitempty"`

	DateSource     string   `json:"date_source,omitempty"`
	DateConfidence string   `json:"date_confidence,omitempty"`
	DateNotes      []string `json:"date_notes,omitempty"`

	// Period is the financial period the document covers, as stated in it
	// ("FY2024", "Q3 2024"), and where that was read.
	Period       string `json:"period,omitempty"`
	PeriodSource string `json:"period_source,omitempty"`

	EvidenceState string `json:"evidence_state,omitempty"`
	AccessNote    string `json:"access_note,omitempty"`

	Rank      int      `json:"rank,omitempty"`
	WhyRanked []string `json:"why_ranked,omitempty"`
	// AlsoSeenAt lists syndicated or copied variants merged into this source.
	AlsoSeenAt []string `json:"also_seen_at,omitempty"`
}

var confOrder = map[string]int{"": 0, ConfNone: 0, ConfLow: 1, ConfMedium: 2, ConfHigh: 3}

var dateLayouts = []string{
	time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02",
	"2006-01", "20060102", time.RFC1123, time.RFC1123Z, time.RFC850, time.ANSIC,
	"January 2, 2006", "Jan 2, 2006", "2 January 2006", "2 Jan 2006", "Monday, January 2, 2006",
}

// ParseDate reads a date in any layout a source commonly uses. A bare date has
// day precision and is read as UTC.
func ParseDate(raw string) (time.Time, bool) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ". ", " ")
	if raw == "" || len(raw) > 64 {
		return time.Time{}, false
	}
	if strings.HasPrefix(raw, "D:") {
		if s := ParsePDFDate(raw); s != "" {
			raw = s
		}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ValidateDate says whether raw is a date that can be trusted at all. A date
// in the future (more than a day ahead, for time zones) or before 1990 is
// metadata noise and is rejected with the reason.
func ValidateDate(raw string, now time.Time) (time.Time, string) {
	t, ok := ParseDate(raw)
	if !ok {
		return time.Time{}, "not a date"
	}
	if t.After(now.Add(24 * time.Hour)) {
		return time.Time{}, "is in the future"
	}
	if t.Year() < 1990 {
		return time.Time{}, "is implausibly old"
	}
	return t, ""
}

var pdfDate = regexp.MustCompile(`^D:(\d{4})(\d{2})?(\d{2})?(\d{2})?(\d{2})?(\d{2})?([Zz]|[+-]\d{2}'?(?:\d{2}'?)?)?`)

// ParsePDFDate converts a PDF information-dictionary date (D:YYYYMMDDHHmmSS+02'00')
// to RFC 3339, keeping the offset. It returns "" if the value is not one.
func ParsePDFDate(raw string) string {
	m := pdfDate.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return ""
	}
	get := func(i int, def string) string {
		if m[i] == "" {
			return def
		}
		return m[i]
	}
	zone := "Z"
	if z := m[7]; z != "" && z != "Z" && z != "z" {
		z = strings.ReplaceAll(z, "'", "")
		if len(z) == 3 {
			z += "00"
		}
		zone = z[:3] + ":" + z[3:]
	}
	out := fmt.Sprintf("%s-%s-%sT%s:%s:%s%s", m[1], get(2, "01"), get(3, "01"), get(4, "00"), get(5, "00"), get(6, "00"), zone)
	if _, err := time.Parse(time.RFC3339, out); err != nil {
		return ""
	}
	return out
}

func sameInstant(a, b time.Time) bool {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d < 36*time.Hour // dates without a time or zone may differ by a day
}

// SetPublished records a publication date from one origin. It never
// overwrites a better-sourced date, never accepts an invalid one, and notes a
// disagreement instead of picking silently: two dates more than a day apart
// leave the more trusted one in place with low confidence and a note.
func (m *Meta) SetPublished(raw, source, conf string, now time.Time) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	t, why := ValidateDate(raw, now)
	if why != "" {
		m.DateNotes = appendNote(m.DateNotes, fmt.Sprintf("published date %q from %s %s and was ignored", clip(raw, 40), source, why))
		return
	}
	if m.PublishedAt == "" {
		m.PublishedAt, m.DateSource, m.DateConfidence = raw, source, conf
		return
	}
	old, _ := ParseDate(m.PublishedAt)
	if sameInstant(old, t) {
		if confOrder[conf] > confOrder[m.DateConfidence] {
			m.PublishedAt, m.DateSource, m.DateConfidence = raw, source, conf
		}
		return
	}
	m.DateNotes = appendNote(m.DateNotes, fmt.Sprintf("dates conflict: %s says %s, %s says %s", m.DateSource, m.PublishedAt, source, raw))
	if confOrder[conf] > confOrder[m.DateConfidence] {
		m.PublishedAt, m.DateSource = raw, source
	}
	m.DateConfidence = ConfLow
}

// SetModified records the date a page says it was last changed. A changed
// page is not a fresh statement: this date never counts as publication.
func (m *Meta) SetModified(raw, source string, now time.Time) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	t, why := ValidateDate(raw, now)
	if why != "" {
		m.DateNotes = appendNote(m.DateNotes, fmt.Sprintf("modified date %q from %s %s and was ignored", clip(raw, 40), source, why))
		return
	}
	if p, ok := ParseDate(m.PublishedAt); ok && t.Before(p.Add(-36*time.Hour)) {
		m.DateNotes = appendNote(m.DateNotes, "modified date is before the published date; modified date ignored")
		return
	}
	m.ModifiedAt = raw
}

// SetFileDates stores the weak hints of a file or response. They are kept
// apart from every other date and never become the publication date.
func (m *Meta) SetFileDates(created, modified, lastModified string, now time.Time) {
	for _, f := range []struct {
		dst  *string
		raw  string
		name string
	}{{&m.FileCreatedAt, created, "file creation date"}, {&m.FileModifiedAt, modified, "file modification date"}, {&m.HTTPLastModified, lastModified, "HTTP Last-Modified"}} {
		raw := strings.TrimSpace(f.raw)
		if raw == "" {
			continue
		}
		if _, why := ValidateDate(raw, now); why != "" {
			m.DateNotes = appendNote(m.DateNotes, f.name+" "+clip(raw, 40)+" "+why+" and was ignored")
			continue
		}
		*f.dst = raw
	}
}

// Finish fixes what must not be left implicit: no date means "unknown", and a
// source with only file dates or a modified date says so.
func (m *Meta) Finish() {
	if m.PublishedAt == "" {
		m.DateSource, m.DateConfidence = DateUnknown, ConfNone
		switch {
		case m.ModifiedAt != "":
			m.DateNotes = appendNote(m.DateNotes, "only an update date is known; when the statement was first published is not")
		case m.FileCreatedAt != "" || m.FileModifiedAt != "" || m.HTTPLastModified != "":
			m.DateNotes = appendNote(m.DateNotes, "only file dates are known; they are not the publication or reporting date")
		}
	}
	if m.EvidenceState == "" {
		m.EvidenceState = StateSnippetOnly
	}
}

func appendNote(list []string, n string) []string {
	for _, e := range list {
		if e == n {
			return list
		}
	}
	if len(list) >= 6 {
		return list
	}
	return append(list, n)
}

func clip(s string, n int) string {
	if len(s) > n {
		return strings.ToValidUTF8(s[:n], "") + "…"
	}
	return s
}

// ---- page metadata -------------------------------------------------------

// maxMetaScan bounds how much markup is looked at for dates.
const maxMetaScan = 512 * 1024

var (
	metaTag    = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attrPair   = regexp.MustCompile(`(?is)\b([a-zA-Z:_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	ldScript   = regexp.MustCompile(`(?is)<script\b[^>]*type\s*=\s*["']application/ld\+json["'][^>]*>(.*?)</script>`)
	timeTag    = regexp.MustCompile(`(?is)<time\b[^>]*\bdatetime\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	publishKey = map[string]bool{"article:published_time": true, "og:published_time": true, "datepublished": true, "date": true, "pubdate": true, "dc.date.issued": true, "dcterms.issued": true, "dc.date": true, "publish-date": true, "citation_publication_date": true, "citation_date": true, "parsely-pub-date": true, "sailthru.date": true}
	modifyKey  = map[string]bool{"article:modified_time": true, "og:updated_time": true, "datemodified": true, "last-modified": true, "dcterms.modified": true, "dc.date.modified": true, "revised": true}
)

// PageDates are the dates a page states about itself.
type PageDates struct {
	Published string
	Modified  string
	Source    string
	Conf      string
}

// ExtractPageDates reads schema.org JSON-LD, Open Graph and article meta tags
// and, as a weak hint only, the first <time> element. The scan is bounded, the
// markup is only parsed, never rendered or fetched from.
func ExtractPageDates(markup string) PageDates {
	if len(markup) > maxMetaScan {
		markup = markup[:maxMetaScan]
	}
	var pd PageDates
	for i, m := range ldScript.FindAllStringSubmatch(markup, 6) {
		_ = i
		if len(m[1]) > 128*1024 {
			continue
		}
		var doc any
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &doc) != nil {
			continue
		}
		budget := 2000
		walkLD(doc, &pd, &budget)
	}
	if pd.Published != "" || pd.Modified != "" {
		pd.Source, pd.Conf = DateJSONLD, ConfHigh
		return pd
	}
	for _, tag := range metaTag.FindAllString(markup, 200) {
		attrs := map[string]string{}
		for _, a := range attrPair.FindAllStringSubmatch(tag, 12) {
			attrs[strings.ToLower(a[1])] = html.UnescapeString(a[2] + a[3])
		}
		key := strings.ToLower(firstNonEmpty(attrs["property"], attrs["name"], attrs["itemprop"]))
		val := attrs["content"]
		switch {
		case publishKey[key] && pd.Published == "":
			pd.Published = val
		case modifyKey[key] && pd.Modified == "":
			pd.Modified = val
		}
	}
	if pd.Published != "" || pd.Modified != "" {
		pd.Source, pd.Conf = DateMeta, ConfHigh
		return pd
	}
	if m := timeTag.FindStringSubmatch(markup); m != nil {
		pd.Published, pd.Source, pd.Conf = html.UnescapeString(m[1]+m[2]), DateTimeElement, ConfLow
	}
	return pd
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func walkLD(v any, pd *PageDates, budget *int) {
	if *budget <= 0 {
		return
	}
	*budget--
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["datePublished"].(string); ok && pd.Published == "" {
			pd.Published = s
		}
		if s, ok := t["dateModified"].(string); ok && pd.Modified == "" {
			pd.Modified = s
		}
		for _, child := range t {
			walkLD(child, pd, budget)
		}
	case []any:
		for _, child := range t {
			walkLD(child, pd, budget)
		}
	}
}

// ---- reporting periods ----------------------------------------------------

// Period is a financial period a document or a question names.
type Period struct {
	Kind    string // "year", "half", "quarter"
	Year    int
	Part    int  // quarter 1-4 or half 1-2
	Derived bool // the quarter was derived from a period-end month, assuming a calendar year
}

// Label is the short form shown to users: FY2024, H1 2024, Q3 2024.
func (p Period) Label() string {
	switch p.Kind {
	case "year":
		return fmt.Sprintf("FY%d", p.Year)
	case "half":
		return fmt.Sprintf("H%d %d", p.Part, p.Year)
	case "quarter":
		return fmt.Sprintf("Q%d %d", p.Part, p.Year)
	}
	return ""
}

// Match results.
const (
	PeriodMatch    = "match"
	PeriodRelated  = "related"  // same year, other granularity (an annual report for a quarter question)
	PeriodMismatch = "mismatch" // another period
	PeriodUnknown  = "unknown"
)

// Compare says how a document period relates to the period asked for.
func Compare(want, got *Period) string {
	if want == nil || got == nil {
		return PeriodUnknown
	}
	switch {
	case want.Kind == got.Kind && want.Year == got.Year && want.Part == got.Part:
		return PeriodMatch
	case want.Year == got.Year && want.Kind != got.Kind:
		return PeriodRelated
	}
	return PeriodMismatch
}

var (
	reQ1       = regexp.MustCompile(`(?i)\bQ([1-4])\s*[-/ ]?\s*(?:FY\s*)?'?(20\d\d)\b`)
	reQ2       = regexp.MustCompile(`(?i)\b(20\d\d)\s*[-/ ]?\s*Q([1-4])\b`)
	reQ3       = regexp.MustCompile(`(?i)\b(first|second|third|fourth)\s+quarter(?:\s+of)?(?:\s+(?:fiscal|FY|year))*\s*,?\s*(20\d\d)\b`)
	reQEnded   = regexp.MustCompile(`(?i)\b(?:quarter|three months)\s+ended\s+(January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{1,2},?\s+(20\d\d)`)
	reHalf     = regexp.MustCompile(`(?i)\b(?:H([12])\s*(20\d\d)|(first|second)\s+half(?:\s+of)?\s+(20\d\d))`)
	reYear     = regexp.MustCompile(`(?i)\b(?:FY\s*'?(20\d\d)|fiscal\s+(?:year\s+)?(20\d\d)|full[- ]year\s+(20\d\d)|annual\s+report\s+(?:for\s+)?(20\d\d)|year\s+ended\s+(?:[A-Za-z]+\s+\d{1,2},?\s+)(20\d\d))`)
	ordinal    = map[string]int{"first": 1, "second": 2, "third": 3, "fourth": 4}
	monthIndex = map[string]int{"january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6, "july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12}
)

// ExtractPeriod finds the first period a text names, quarters before halves
// before years. Only what the text states is returned; a quarter derived from
// a "quarter ended" month assumes a calendar year and says so (Derived).
func ExtractPeriod(text string) (*Period, bool) {
	if len(text) > 4000 {
		text = text[:4000]
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	if m := reQ1.FindStringSubmatch(text); m != nil {
		return &Period{Kind: "quarter", Year: atoi(m[2]), Part: atoi(m[1])}, true
	}
	if m := reQ2.FindStringSubmatch(text); m != nil {
		return &Period{Kind: "quarter", Year: atoi(m[1]), Part: atoi(m[2])}, true
	}
	if m := reQ3.FindStringSubmatch(text); m != nil {
		return &Period{Kind: "quarter", Year: atoi(m[2]), Part: ordinal[strings.ToLower(m[1])]}, true
	}
	if m := reQEnded.FindStringSubmatch(text); m != nil {
		return &Period{Kind: "quarter", Year: atoi(m[2]), Part: (monthIndex[strings.ToLower(m[1])]-1)/3 + 1, Derived: true}, true
	}
	if m := reHalf.FindStringSubmatch(text); m != nil {
		if m[1] != "" {
			return &Period{Kind: "half", Year: atoi(m[2]), Part: atoi(m[1])}, true
		}
		return &Period{Kind: "half", Year: atoi(m[4]), Part: ordinal[strings.ToLower(m[3])]}, true
	}
	if m := reYear.FindStringSubmatch(text); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return &Period{Kind: "year", Year: atoi(g)}, true
			}
		}
	}
	return nil, false
}

var coverDate = regexp.MustCompile(`(?i)\b(?:dated|date of report|published|published on|filed|issued|report date)\s*:?\s*((?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec)\.?\s+\d{1,2},?\s+\d{4}|\d{1,2}\s+(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{4}|\d{4}-\d{2}-\d{2})`)

// CoverDate reads a stated report date near the start of a document ("Published:
// October 30, 2024"). It looks only at the cover text, requires a label, and
// returns "" otherwise: a bare date in a document is not its publication date.
func CoverDate(text string) string {
	if len(text) > 2500 {
		text = text[:2500]
	}
	if m := coverDate.FindStringSubmatch(text); m != nil {
		return strings.ReplaceAll(strings.TrimSpace(m[1]), "Sept ", "Sep ")
	}
	return ""
}
