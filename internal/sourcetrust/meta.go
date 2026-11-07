package sourcetrust

import "time"

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
