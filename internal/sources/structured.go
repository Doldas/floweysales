package sources

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// maxStructuredSources bounds the sources one SEC listing or facts result adds.
const maxStructuredSources = 10

// registerFilings records each filing of a get_sec_filings result as a source
// the agent read: the record (form, filing date, period, accession, official
// URL) is exactly what reached the model. The model gets a source_id in each
// filing so a sentence about a filing can cite it as [Sn].
func (r *Registry) registerFilings(callID, result string) (string, []Source) {
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil {
		return result, nil
	}
	var items []map[string]any
	if json.Unmarshal(doc["filings"], &items) != nil || len(items) == 0 {
		return result, nil
	}
	var retrieved string
	_ = json.Unmarshal(doc["retrieved_at"], &retrieved)
	var added []Source
	for _, it := range items {
		if len(added) >= maxStructuredSources {
			break
		}
		link, _ := it["document_url"].(string)
		accession, _ := it["accession"].(string)
		if link == "" && accession == "" {
			continue
		}
		form, _ := it["form"].(string)
		filed, _ := it["filed"].(string)
		period, _ := it["period_end"].(string)
		excerpt := fmt.Sprintf("%s filing, accession %s, filed %s", form, accession, filed)
		if period != "" {
			excerpt += ", period ending " + period
		}
		s := r.add(Source{Tool: "get_sec_filings", CallID: callID, URL: link, Title: strings.TrimSpace(form + " " + filed),
			Class: ClassPrimaryFiling, Access: AccessRead, DocumentID: accession, PublishedAt: filed, RetrievedAt: retrieved, Excerpt: excerpt})
		it["source_id"] = s.ID
		added = append(added, s)
	}
	if len(added) == 0 {
		return result, nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return result, added
	}
	doc["filings"] = raw
	out, err := json.Marshal(doc)
	if err != nil {
		return result, added
	}
	return string(out), added
}

// registerFacts records the filings behind a get_sec_facts result: one source
// per accession, whose excerpt lists the facts the model received from it
// (metric, value, unit, period), so a sentence citing it is checked against
// those values. Each fact gets the source_id of its filing.
func (r *Registry) registerFacts(callID, result string) (string, []Source) {
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil {
		return result, nil
	}
	var facts []map[string]any
	if json.Unmarshal(doc["facts"], &facts) != nil || len(facts) == 0 {
		return result, nil
	}
	var retrieved string
	_ = json.Unmarshal(doc["retrieved_at"], &retrieved)
	type group struct {
		accession, link, form, filed string
		lines                        []string
		idx                          []int
	}
	groups := map[string]*group{}
	var order []string
	for i, f := range facts {
		accession, _ := f["accession"].(string)
		link, _ := f["filing_url"].(string)
		key := accession
		if key == "" {
			key = link
		}
		if key == "" {
			continue
		}
		g := groups[key]
		if g == nil {
			g = &group{accession: accession, link: link}
			g.form, _ = f["form"].(string)
			g.filed, _ = f["filed"].(string)
			groups[key] = g
			order = append(order, key)
		}
		metric, _ := f["metric"].(string)
		if metric == "" {
			metric, _ = f["concept"].(string)
		}
		unit, _ := f["unit"].(string)
		start, _ := f["start"].(string)
		end, _ := f["end"].(string)
		period, _ := f["fiscal_period"].(string)
		value, _ := f["value"].(float64)
		line := fmt.Sprintf("%s: %s %s", metric, strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), "."), unit)
		if start != "" {
			line += " from " + start
		}
		if end != "" {
			line += " to " + end
		}
		if fy, ok := f["fiscal_year"].(float64); ok && fy > 0 {
			line += fmt.Sprintf(" (FY%d %s)", int(fy), period)
		}
		g.lines = append(g.lines, line)
		g.idx = append(g.idx, i)
	}
	if len(order) == 0 {
		return result, nil
	}
	// The newest filings first: those are the ones an answer cites.
	sort.SliceStable(order, func(a, b int) bool { return groups[order[a]].filed > groups[order[b]].filed })
	var added []Source
	for _, key := range order {
		if len(added) >= maxStructuredSources {
			break
		}
		g := groups[key]
		excerpt := strings.Join(g.lines, "\n")
		s := r.add(Source{Tool: "get_sec_facts", CallID: callID, URL: g.link, Title: strings.TrimSpace(g.form + " " + g.filed + " XBRL facts"),
			Class: ClassPrimaryFiling, Access: AccessRead, DocumentID: g.accession, PublishedAt: g.filed, RetrievedAt: retrieved, Excerpt: excerpt})
		for _, i := range g.idx {
			facts[i]["source_id"] = s.ID
		}
		added = append(added, s)
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return result, added
	}
	doc["facts"] = raw
	out, err := json.Marshal(doc)
	if err != nil {
		return result, added
	}
	return string(out), added
}
