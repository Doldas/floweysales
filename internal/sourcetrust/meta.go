package sourcetrust

import (
	"strings"
	"time"
)

// ForSearchResult builds the metadata of a search hit. Only what the provider
// returned is used: a date from the provider is medium confidence (it may come
// from the page or from the crawl), and a period comes only from the words of
// the title and snippet.
func ForSearchResult(rawURL, title, snippet, providerPublished, retrievedAt string, now time.Time) *Meta {
	c := Classify(rawURL, title, snippet)
	m := &Meta{Type: c.Type, Role: c.Role, TypeBasis: c.Basis, RetrievedAt: retrievedAt, EvidenceState: StateSnippetOnly}
	m.SetPublished(providerPublished, DateSearchProvider, ConfMedium, now)
	if p, ok := ExtractPeriod(title + " " + snippet); ok {
		m.Period, m.PeriodSource = p.Label(), "title_snippet"
	}
	m.Finish()
	return m
}

// ForFiling builds the metadata of an SEC filing section, whose filing date
// comes from the official filing index.
func ForFiling(form, filed, retrievedAt string, now time.Time) *Meta {
	c, _ := ClassifyTool("get_sec_section")
	m := &Meta{Type: c.Type, Role: c.Role, TypeBasis: c.Basis, RetrievedAt: retrievedAt, EvidenceState: StatePageRead}
	m.SetPublished(filed, DateFilingIndex, ConfHigh, now)
	if p, ok := ExtractPeriod(form); ok {
		m.Period, m.PeriodSource = p.Label(), "filing_form"
	}
	m.Finish()
	return m
}

// ApplyPage merges what reading a page showed: dates the page states about
// itself, the period its opening text names, and the file dates. File dates
// stay in their own fields. Returns the meta for chaining.
func (m *Meta) ApplyPage(markup, text string, isPDF bool, fileCreated, fileModified, lastModified string, now time.Time) *Meta {
	if !isPDF {
		pd := ExtractPageDates(markup)
		m.SetPublished(pd.Published, pd.Source, pd.Conf, now)
		m.SetModified(pd.Modified, pd.Source, now)
	} else if d := CoverDate(text); d != "" {
		m.SetPublished(d, DatePDFCover, ConfMedium, now)
	}
	m.SetFileDates(fileCreated, fileModified, lastModified, now)
	if p, ok := ExtractPeriod(text); ok {
		m.Period, m.PeriodSource = p.Label(), "document_text"
	}
	if isPDF {
		m.EvidenceState = StatePDFText
	} else {
		m.EvidenceState = StatePageRead
	}
	m.Finish()
	return m
}

// MergeRead folds the result of reading a source into the metadata it had as a
// search hit. A date found on the page is checked against the provider's date
// (disagreement is recorded, not resolved by guessing), file dates stay in
// their own fields, and the evidence state moves from snippet to what was read.
func (m *Meta) MergeRead(r *Meta, now time.Time) {
	if r == nil {
		return
	}
	m.SetPublished(r.PublishedAt, r.DateSource, r.DateConfidence, now)
	m.SetModified(r.ModifiedAt, r.DateSource, now)
	if r.EffectiveAt != "" {
		m.EffectiveAt = r.EffectiveAt
	}
	for _, f := range []struct {
		dst *string
		v   string
	}{{&m.FileCreatedAt, r.FileCreatedAt}, {&m.FileModifiedAt, r.FileModifiedAt}, {&m.HTTPLastModified, r.HTTPLastModified}} {
		if f.v != "" {
			*f.dst = f.v
		}
	}
	for _, n := range r.DateNotes {
		if !strings.HasPrefix(n, "only ") {
			m.DateNotes = appendNote(m.DateNotes, n)
		}
	}
	if r.Period != "" {
		m.Period, m.PeriodSource = r.Period, r.PeriodSource
	}
	if r.RetrievedAt != "" {
		m.RetrievedAt = r.RetrievedAt
	}
	if r.EvidenceState != "" {
		m.EvidenceState = r.EvidenceState
	}
	m.Finish()
}

// MarkReadFailure records that a source could not be read. A refusal by the
// site (HTTP 403 or 429) is its own state: the source stays a lead, and
// nothing of it may be claimed as read.
func (m *Meta) MarkReadFailure(reason string) {
	low := strings.ToLower(reason)
	switch {
	case strings.Contains(low, "403") || strings.Contains(low, "429") || strings.Contains(low, "denied") || strings.Contains(low, "denying"):
		m.EvidenceState = StateAccessDenied
		m.AccessNote = "The site refused automated access; only the search snippet was seen."
	default:
		m.EvidenceState = StateReadFailed
		m.AccessNote = "The page could not be read; only the search snippet was seen."
	}
}
