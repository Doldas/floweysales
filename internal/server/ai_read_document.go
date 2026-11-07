package server

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/vision"
)

const (
	// readVisionTextBudget is the room in a read_source result for figures the
	// vision model read and the deterministic checks verified.
	readVisionTextBudget = 3000
	// viewerSparseChars: an HTML page with less text than this is treated as
	// a viewer shell that probably wraps a PDF.
	viewerSparseChars = 1500
	maxPDFLinks       = 5
)

// documentPage is one page of a PDF as reported to the agent and the UI.
type documentPage struct {
	Page      int    `json:"page"`
	Class     string `json:"class"`  // text, hybrid, scanned or empty
	Method    string `json:"method"` // text, vision, text+vision or none
	TextChars int    `json:"text_chars"`
	Vision    string `json:"vision,omitempty"` // ok, cached, failed, skipped, unavailable
	Note      string `json:"note,omitempty"`
}

// visualEvidence is the vision model's reading of one page, kept apart from
// the text layer and from any later analysis.
type visualEvidence struct {
	Page         int                  `json:"page"`
	Observations []vision.Observation `json:"observations"`
	Limitations  string               `json:"limitations,omitempty"`
	Cached       bool                 `json:"cached,omitempty"`
}

// documentReport is the PDF part of a read_source result.
type documentReport struct {
	Type          string           `json:"type"`
	PagesTotal    int              `json:"pages_total"`
	PagesExamined int              `json:"pages_examined"`
	Pages         []documentPage   `json:"pages"`
	VisionStatus  string           `json:"vision_status"`
	VisionBudget  *vision.Snapshot `json:"vision_budget,omitempty"`
	Evidence      map[string]any   `json:"evidence"`
	Limitations   []string         `json:"limitations,omitempty"`
}

// ---- HTML viewer pages ----------------------------------------------------

var (
	pdfAttrTag   = regexp.MustCompile(`(?is)<(a|iframe|embed|object|link|source)\b([^>]*)>`)
	pdfAttrValue = regexp.MustCompile(`(?is)\b(href|src|data)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	pdfTypeAttr  = regexp.MustCompile(`(?i)type\s*=\s*["']?application/pdf`)
	pdfKeyword   = regexp.MustCompile(`(?i)annual|interim|quarter|report|rapport|q[1-4]|[1-4]q|10-?[kq]|delrapport|bokslut|financial|results`)
)

// findPDFLinks lists the PDF files an HTML page links to or embeds, restricted
// to the page's own site: a report viewer on an investor-relations page points
// at that company's file, not at an arbitrary host. Candidates that look like
// reports come first. It reads only the markup it was given; nothing is fetched.
func findPDFLinks(body, base string) []string {
	bu, err := url.Parse(base)
	if err != nil {
		return nil
	}
	type cand struct {
		url   string
		score int
		order int
	}
	seen := map[string]bool{}
	var found []cand
	add := func(raw string, typed bool) {
		raw = strings.TrimSpace(html.UnescapeString(raw))
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			return
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		abs := bu.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			return
		}
		if !typed && !strings.HasSuffix(strings.ToLower(abs.Path), ".pdf") {
			return
		}
		abs.Fragment = ""
		if !sameSite(bu.Hostname(), abs.Hostname()) || seen[abs.String()] {
			return
		}
		seen[abs.String()] = true
		score := len(pdfKeyword.FindAllString(abs.String(), -1))
		found = append(found, cand{abs.String(), score, len(found)})
	}
	for _, m := range pdfAttrTag.FindAllStringSubmatch(body, 400) {
		typed := pdfTypeAttr.MatchString(m[2])
		for _, v := range pdfAttrValue.FindAllStringSubmatch(m[2], -1) {
			val := v[2] + v[3]
			add(val, typed)
			// A viewer address names its file in a parameter: viewer.html?file=/x.pdf
			if ref, err := url.Parse(strings.TrimSpace(html.UnescapeString(val))); err == nil {
				for _, key := range []string{"file", "url", "src", "doc", "pdf"} {
					if p := ref.Query().Get(key); p != "" {
						add(p, false)
					}
				}
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	out := []string{}
	for _, c := range found {
		if len(out) == maxPDFLinks {
			break
		}
		out = append(out, c.url)
	}
	return out
}

// sameSite reports whether two hosts belong to the same site: equal, or with
// the same registrable domain (ir.example.com and www.example.com).
func sameSite(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || registrable(a) == registrable(b)
}

func registrable(host string) string {
	parts := strings.Split(strings.TrimSuffix(host, "."), ".")
	n := len(parts)
	if n <= 2 {
		return host
	}
	// co.uk, com.au and similar two-level public suffixes.
	if len(parts[n-1]) == 2 && map[string]bool{"co": true, "com": true, "org": true, "net": true, "gov": true, "ac": true}[parts[n-2]] {
		return strings.Join(parts[n-3:], ".")
	}
	return strings.Join(parts[n-2:], ".")
}

// looksLikeViewer is true for an address that is a document viewer shell.
func looksLikeViewer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	if strings.Contains(p, "viewer") || strings.Contains(p, "/pdfjs/") {
		return true
	}
	q := u.Query()
	for _, k := range []string{"file", "pdf"} {
		if strings.HasSuffix(strings.ToLower(q.Get(k)), ".pdf") {
			return true
		}
	}
	return false
}

// ---- PDF documents --------------------------------------------------------

type visionRunKey struct{}

// visionRunBudget returns the vision budget of the current agent run: one per
// run, shared by every document and every team member in it. Outside an agent
// run the budget lasts for the one call.
func visionRunBudget(ctx context.Context, cfg vision.Config) *vision.Budget {
	if scope := ai.RunScopeFrom(ctx); scope != nil {
		return scope.GetOrCreate(visionRunKey{}, func() any { return vision.NewBudget(cfg.Limits) }).(*vision.Budget)
	}
	return vision.NewBudget(cfg.Limits)
}

func (e *Engine) visionScope(ctx context.Context) string {
	if u, ok := e.agentUser(ctx); ok {
		return fmt.Sprintf("user:%d", u.ID)
	}
	return "anonymous"
}

type pdfRead struct {
	Text      string
	Truncated bool
	Report    documentReport
	Evidence  []visualEvidence
}

// readPDF turns a PDF into page-labelled text and, where the text layer is
// missing or a page carries a picture, page-cited visual evidence from the
// configured vision model. Every limitation is reported; nothing is guessed.
func (e *Engine) readPDF(ctx context.Context, data []byte) (pdfRead, error) {
	doc, err := parsePDFPages(data)
	if err != nil {
		return pdfRead{}, err
	}
	rep := documentReport{Type: "pdf", PagesTotal: doc.TotalPages, PagesExamined: len(doc.Pages)}
	if doc.Truncated {
		rep.Limitations = append(rep.Limitations, fmt.Sprintf("only the first %d of %d pages were examined", readPDFMaxPages, doc.TotalPages))
	}
	pages := make([]documentPage, len(doc.Pages))
	for i, p := range doc.Pages {
		pages[i] = documentPage{Page: p.Number, Class: p.Class, Method: "none", TextChars: len(p.Text)}
		if p.Class == pageText || p.Class == pageHybrid {
			pages[i].Method = "text"
		}
	}

	cfg := e.vision.config()
	rep.VisionStatus = cfg.Status()
	var evidence []visualEvidence
	var wanted []int // indexes into doc.Pages, scanned pages first
	for _, class := range []string{pageScanned, pageHybrid} {
		for i, p := range doc.Pages {
			if p.Class == class {
				wanted = append(wanted, i)
			}
		}
	}
	switch {
	case len(wanted) == 0:
		rep.VisionStatus = "not_needed"
	case !cfg.Usable():
		for _, i := range wanted {
			pages[i].Vision = "unavailable"
			if doc.Pages[i].Class == pageScanned {
				pages[i].Note = visionUnavailableNote(cfg)
			}
		}
		if n := countClass(doc.Pages, pageScanned); n > 0 {
			rep.Limitations = append(rep.Limitations, fmt.Sprintf("%d scanned page(s) have no text layer and were not read: %s", n, visionUnavailableNote(cfg)))
		}
		if n := countClass(doc.Pages, pageHybrid); n > 0 {
			rep.Limitations = append(rep.Limitations, fmt.Sprintf("%d page(s) contain pictures (charts or tables as images) that were not interpreted: %s", n, visionUnavailableNote(cfg)))
		}
	default:
		evidence = e.runVision(ctx, cfg, doc, wanted, pages, &rep)
	}
	for _, p := range doc.Pages {
		if p.Class == pageEmpty {
			rep.Limitations = append(rep.Limitations, fmt.Sprintf("page %d has neither a text layer nor an extractable picture (vector graphics are not rendered)", p.Number))
			break
		}
	}

	// Page-labelled text, then the verified figures the vision model read.
	var b strings.Builder
	truncated := false
	for _, p := range doc.Pages {
		if p.Text == "" {
			continue
		}
		line := fmt.Sprintf("[page %d] %s\n", p.Number, p.Text)
		if b.Len()+len(line) > readPageMaxText {
			room := readPageMaxText - b.Len()
			if room > 40 {
				b.WriteString(strings.ToValidUTF8(line[:room], ""))
			}
			truncated = true
			break
		}
		b.WriteString(line)
	}
	if v := visionLines(evidence); v != "" {
		b.WriteString("\n[figures read from page images by the vision model; checked by deterministic rules, not independently confirmed]\n" + v)
	}
	verified, unverified := countObservations(evidence)
	rep.Pages = pages
	rep.Evidence = map[string]any{
		"retrieved":          true,
		"text_parsed":        countClass(doc.Pages, pageText)+countClass(doc.Pages, pageHybrid) > 0,
		"vision_interpreted": len(evidence) > 0,
		"verified_values":    verified,
		"unverified_values":  unverified,
		"note":               "verified_values passed the deterministic checks (parsed value, printed in its source text, stated period, consistent totals); none was confirmed against an independent data source",
	}
	return pdfRead{Text: strings.TrimSpace(b.String()), Truncated: truncated, Report: rep, Evidence: evidence}, nil
}

func countClass(pages []pdfPageInfo, class string) int {
	n := 0
	for _, p := range pages {
		if p.Class == class {
			n++
		}
	}
	return n
}

func countObservations(ev []visualEvidence) (verified, unverified int) {
	for _, pg := range ev {
		for _, o := range pg.Observations {
			if o.Verified {
				verified++
			} else {
				unverified++
			}
		}
	}
	return
}

func visionUnavailableNote(cfg vision.Config) string {
	switch cfg.Status() {
	case "not_configured":
		return "no vision model is configured (a SYS_ADMIN can set one up under Settings, Vision model)"
	case "no_image_support_declared":
		return "the configured model is not marked as accepting images"
	case "needs_test":
		return "the vision model has not passed its capability test yet"
	case "disabled":
		return "the vision model is switched off"
	}
	return "the vision model is unavailable"
}

// visionLines renders verified observations as page-cited lines of text.
func visionLines(ev []visualEvidence) string {
	var b strings.Builder
	for _, pg := range ev {
		for _, o := range pg.Observations {
			if !o.Verified {
				continue
			}
			unit := strings.TrimSpace(o.Unit + " " + o.Currency)
			line := fmt.Sprintf("[page %d, vision] %s: %s", o.Page, o.Label, o.OriginalValue)
			if unit != "" {
				line += " " + unit
			}
			if o.Period != "" {
				line += " (" + o.Period + ")"
			}
			line += "\n"
			if b.Len()+len(line) > readVisionTextBudget {
				return b.String()
			}
			b.WriteString(line)
		}
	}
	return b.String()
}

// runVision sends the chosen page pictures to the vision model within the
// document and run limits, and records the outcome per page.
func (e *Engine) runVision(ctx context.Context, cfg vision.Config, doc pdfDocument, wanted []int, pages []documentPage, rep *documentReport) []visualEvidence {
	lim := cfg.Limits
	budget := visionRunBudget(ctx, cfg)
	cache := e.vision.resultCache()
	scope := e.visionScope(ctx)

	client := vision.NewClient(cfg)
	client.OnResult = func(u *vision.Usage, err error) {
		e.vision.record(func(s *visionStats) {
			s.Calls++
			if u == nil {
				s.TokensUnknownCalls++
			} else {
				s.Tokens += u.PromptTokens + u.CompletionTokens
			}
			if err != nil {
				s.Failures++
				s.LastErrorCode, s.LastErrorAt = vision.CodeOf(err), time.Now().UTC().Format(time.RFC3339)
			}
		})
	}

	type job struct {
		idx int
		img pdfImage
	}
	var jobs []job
	for _, i := range wanted {
		p := doc.Pages[i]
		if len(jobs) >= lim.MaxPagesPerDocument {
			pages[i].Vision, pages[i].Note = "skipped", fmt.Sprintf("over the limit of %d vision pages per document", lim.MaxPagesPerDocument)
			continue
		}
		if len(p.Images) == 0 {
			pages[i].Vision = "skipped"
			pages[i].Note = firstNonEmpty(p.Note, "no picture on the page could be extracted")
			continue
		}
		best := p.Images[0]
		for _, im := range p.Images[1:] {
			if im.Width*im.Height > best.Width*best.Height {
				best = im
			}
		}
		switch {
		case len(best.Data) > lim.MaxImageBytes:
			pages[i].Vision, pages[i].Note = "skipped", fmt.Sprintf("the page picture is larger than %d MB", lim.MaxImageBytes>>20)
		case best.Width*best.Height > lim.MaxImagePixels:
			pages[i].Vision, pages[i].Note = "skipped", "the page picture has too many pixels"
		default:
			jobs = append(jobs, job{i, best})
		}
	}

	type result struct {
		idx    int
		page   vision.Page
		cached bool
	}
	results := make(chan result, len(jobs))
	sem := make(chan struct{}, lim.Concurrency)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- result{j.idx, vision.Page{Page: doc.Pages[j.idx].Number, Error: "cancelled", ErrorCode: vision.CodeTimeout}, false}
				return
			}
			key := vision.Key(scope, cfg.Model, j.img.Data)
			if hit, ok := cache.Get(key); ok {
				hit.Page = doc.Pages[j.idx].Number
				for k := range hit.Observations {
					hit.Observations[k].Page = hit.Page
				}
				e.vision.record(func(s *visionStats) { s.CacheHits++ })
				results <- result{j.idx, hit, true}
				return
			}
			callCtx, cancel := context.WithTimeout(ctx, time.Duration(lim.TimeoutSeconds)*time.Second)
			defer cancel()
			e.vision.record(func(s *visionStats) { s.Images++ })
			pg := vision.ExtractPage(callCtx, client, budget, vision.Image{Mime: j.img.Mime, Data: j.img.Data, Page: doc.Pages[j.idx].Number})
			cache.Put(key, pg)
			results <- result{j.idx, pg, false}
		}(j)
	}
	wg.Wait()
	close(results)

	var evidence []visualEvidence
	for r := range results {
		pg := r.page
		if pg.Error != "" {
			pages[r.idx].Vision, pages[r.idx].Note = "failed", pg.Error
			rep.Limitations = append(rep.Limitations, fmt.Sprintf("page %d: vision extraction failed (%s)", pg.Page, pg.Error))
			continue
		}
		pages[r.idx].Vision = "ok"
		if r.cached {
			pages[r.idx].Vision = "cached"
		}
		if pages[r.idx].Class == pageScanned {
			pages[r.idx].Method = "vision"
		} else {
			pages[r.idx].Method = "text+vision"
		}
		evidence = append(evidence, visualEvidence{Page: pg.Page, Observations: pg.Observations, Limitations: pg.Limitations, Cached: r.cached})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].Page < evidence[j].Page })
	snap := budget.Snapshot()
	rep.VisionBudget = &snap
	rep.VisionStatus = "used"
	if len(evidence) == 0 {
		rep.VisionStatus = "failed"
	}
	if snap.TokensUnknown {
		rep.Limitations = append(rep.Limitations, "the vision endpoint reported no token usage for some calls, so the token cap cannot be enforced for them")
	}
	return evidence
}
