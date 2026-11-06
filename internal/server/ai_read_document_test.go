package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
	"floweysales/internal/vision"
)

// testJPEG is a small picture; its content is irrelevant to the fake model.
func testJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

// pdfWithPages builds a PDF whose pages each have optional text and an
// optional JPEG picture drawn on it.
type testPage struct {
	text string
	jpeg []byte
	w, h int
}

func pdfWithPages(pages []testPage) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offsets []int
	obj := func(body string) int {
		offsets = append(offsets, b.Len())
		n := len(offsets)
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	// 1 catalog, 2 pages, 3 font; then per page: image?, content, page.
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, len(pages))
	obj("PAGES_PLACEHOLDER")
	pagesIdx := 1
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, p := range pages {
		res := "/Font << /F1 3 0 R >>"
		content := ""
		if p.text != "" {
			content += "BT /F1 12 Tf 72 720 Td (" + p.text + ") Tj ET\n"
		}
		if p.jpeg != nil {
			offsets = append(offsets, b.Len())
			n := len(offsets)
			fmt.Fprintf(&b, "%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", n, p.w, p.h, len(p.jpeg))
			b.Write(p.jpeg)
			b.WriteString("\nendstream\nendobj\n")
			res += fmt.Sprintf(" /XObject << /Im%d %d 0 R >>", i, n)
			content += fmt.Sprintf("q 400 0 0 400 50 200 cm /Im%d Do Q\n", i)
		}
		c := obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		pg := obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << %s >> >>", c, res))
		kids[i] = fmt.Sprintf("%d 0 R", pg)
	}
	real := fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	return rebuildPDF(b.String(), "PAGES_PLACEHOLDER", real, pagesIdx)
}

// rebuildPDF substitutes an object body and recomputes every xref offset.
func rebuildPDF(doc, ph, real string, _ int) []byte {
	doc = strings.Replace(doc, ph, real, 1)
	var b bytes.Buffer
	var offsets []int
	body := []byte(doc)
	for i := 0; ; {
		j := bytes.Index(body[i:], []byte(" 0 obj\n"))
		if j < 0 {
			break
		}
		start := i + j
		for start > 0 && body[start-1] >= '0' && body[start-1] <= '9' {
			start--
		}
		offsets = append(offsets, start)
		i = i + j + 7
	}
	b.Write(body)
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

const longText = "Interim report January to September 2024. Net sales for the group amounted to 4300 million and operating profit was 600 million for the period."

func TestParsePDFPagesClassifiesTextScannedAndHybrid(t *testing.T) {
	j := testJPEG(300, 300)
	data := pdfWithPages([]testPage{
		{text: longText},
		{jpeg: j, w: 300, h: 300},
		{text: longText, jpeg: j, w: 300, h: 300},
		{},
	})
	doc, err := parsePDFPages(data)
	if err != nil || doc.TotalPages != 4 {
		t.Fatalf("%v %+v", err, doc)
	}
	var classes []string
	for _, p := range doc.Pages {
		classes = append(classes, p.Class)
	}
	if got := strings.Join(classes, ","); got != "text,scanned,hybrid,empty" {
		t.Fatalf("classes %s", got)
	}
	if !strings.Contains(doc.Pages[0].Text, "Net sales") {
		t.Errorf("text page: %q", doc.Pages[0].Text)
	}
	for _, i := range []int{1, 2} {
		if len(doc.Pages[i].Images) != 1 || doc.Pages[i].Images[0].Mime != "image/jpeg" || !bytes.Equal(doc.Pages[i].Images[0].Data, j) {
			t.Errorf("page %d images: %+v note %q", i+1, doc.Pages[i].Images, doc.Pages[i].Note)
		}
	}
	if len(doc.Pages[0].Images) != 0 {
		t.Error("text pages need no pictures")
	}
}

func TestParsePDFPagesRejectsCorruptAndMislabeled(t *testing.T) {
	for _, bad := range [][]byte{[]byte("<html>blocked</html>"), []byte("%PDF-1.4 garbage"), nil} {
		if _, err := parsePDFPages(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
}

// visionServer answers like a multimodal model and counts requests.
func visionServer(t *testing.T, calls *int32, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		var req struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		sawImage := false
		for _, m := range req.Messages {
			if arr, ok := m.Content.([]any); ok {
				for _, p := range arr {
					if p.(map[string]any)["type"] == "image_url" {
						sawImage = true
					}
				}
			}
		}
		if !sawImage {
			http.Error(w, `{"error":{"message":"image missing"}}`, 400)
			return
		}
		b, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": reply}}},
			"usage":   map[string]int{"prompt_tokens": 500, "completion_tokens": 50},
		})
		_, _ = w.Write(b)
	}))
}

const chartReply = `{"observations":[
 {"kind":"table_cell","group":"Net sales","role":"component","label":"Nordics","original_value":"1 000","unit":"MSEK","period":"Q3 2024","period_kind":"quarter","source_excerpt":"Nordics 1 000","uncertainty":"low"},
 {"kind":"table_cell","group":"Net sales","role":"component","label":"Europe","original_value":"500","unit":"MSEK","period":"Q3 2024","period_kind":"quarter","source_excerpt":"Europe 500","uncertainty":"low"},
 {"kind":"table_cell","group":"Net sales","role":"total","label":"Total","original_value":"1 500","unit":"MSEK","period":"Q3 2024","period_kind":"quarter","source_excerpt":"Total 1 500","uncertainty":"low"},
 {"kind":"figure_value","label":"Operating profit FY","original_value":"(12,5)","unit":"MSEK","period":"Full year 2023","period_kind":"quarter","source_excerpt":"Operating profit (12,5)","uncertainty":"low"}
],"limitations":"footnotes are cut off"}`

func visionEngine(t *testing.T, url string) *Engine {
	t.Helper()
	e := &Engine{}
	cfg, err := vision.Config{Enabled: true, BaseURL: url, Model: "vis", APIKey: "k", SupportsImages: true}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	cfg.VerifiedFingerprint = cfg.Fingerprint()
	e.vision.set(cfg)
	return e
}

func TestTextPDFNeedsNoVisionCall(t *testing.T) {
	var calls int32
	srv := visionServer(t, &calls, chartReply)
	defer srv.Close()
	e := visionEngine(t, srv.URL)
	res, err := e.readPDF(context.Background(), pdfWithPages([]testPage{{text: longText}, {text: "Second page of the interim report with more words to pass the sparse threshold of the classifier."}}))
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 0 || res.Report.VisionStatus != "not_needed" {
		t.Fatalf("calls=%d status=%s", calls, res.Report.VisionStatus)
	}
	if !strings.Contains(res.Text, "[page 1] ") || !strings.Contains(res.Text, "[page 2] ") || res.Report.Pages[1].Method != "text" {
		t.Fatalf("%q %+v", res.Text, res.Report.Pages)
	}
}

func TestScannedAndHybridPagesRouteThroughVisionWithPageCitations(t *testing.T) {
	var calls int32
	srv := visionServer(t, &calls, chartReply)
	defer srv.Close()
	e := visionEngine(t, srv.URL)
	j := testJPEG(300, 300)
	data := pdfWithPages([]testPage{{text: longText}, {jpeg: j, w: 300, h: 300}, {text: longText, jpeg: j, w: 300, h: 300}})
	ctx := context.Background()
	res, err := e.readPDF(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 2 || res.Report.VisionStatus != "used" {
		t.Fatalf("calls=%d %s", calls, res.Report.VisionStatus)
	}
	if len(res.Evidence) != 2 || res.Evidence[0].Page != 2 || res.Evidence[1].Page != 3 {
		t.Fatalf("%+v", res.Evidence)
	}
	pg := res.Report.Pages
	if pg[0].Method != "text" || pg[1].Method != "vision" || pg[2].Method != "text+vision" || pg[1].Vision != "ok" {
		t.Fatalf("%+v", pg)
	}
	// Numbers keep their printed form; totals are checked; an annual figure
	// labelled as a quarter is not verified.
	byLabel := map[string]vision.Observation{}
	for _, o := range res.Evidence[0].Observations {
		byLabel[o.Label] = o
	}
	if o := byLabel["Total"]; !o.Verified || o.Value == nil || *o.Value != 1500 || o.OriginalValue != "1 500" || o.Page != 2 {
		t.Errorf("total: %+v", o)
	}
	if o := byLabel["Operating profit FY"]; o.Verified || o.Sign != "negative" || !hasFlag(o.Flags, "annual_figure_labelled_quarter") {
		t.Errorf("annual figure: %+v", o)
	}
	if !strings.Contains(res.Text, "[page 2, vision] Total: 1 500 MSEK (Q3 2024)") || strings.Contains(res.Text, "Operating profit FY") {
		t.Errorf("only verified figures enter the text: %q", res.Text)
	}
	if res.Report.Evidence["vision_interpreted"] != true || res.Report.Evidence["verified_values"].(int) != 6 {
		t.Errorf("%+v", res.Report.Evidence)
	}

	// The same unchanged pages are served from the cache, not paid for again.
	res2, _ := e.readPDF(ctx, data)
	if atomic.LoadInt32(&calls) != 2 || res2.Report.Pages[1].Vision != "cached" {
		t.Fatalf("calls=%d %+v", calls, res2.Report.Pages)
	}
	// A different user does not share that cache.
	other := context.WithValue(ctx, agentUserKey{}, userForTest(7))
	if _, err := e.readPDF(other, data); err != nil || atomic.LoadInt32(&calls) != 4 {
		t.Fatalf("cross-user reuse: calls=%d %v", calls, err)
	}
}

func userForTest(id int64) identity.User { return identity.User{ID: id} }

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

func hasFlag(f []string, v string) bool {
	for _, x := range f {
		if x == v {
			return true
		}
	}
	return false
}

func TestScannedPDFWithoutUsableVisionFailsExplicitly(t *testing.T) {
	j := testJPEG(300, 300)
	data := pdfWithPages([]testPage{{jpeg: j, w: 300, h: 300}})
	e := &Engine{} // no vision model configured
	res, err := e.readPDF(context.Background(), data)
	if err != nil || res.Text != "" {
		t.Fatalf("%v %q", err, res.Text)
	}
	if res.Report.Pages[0].Vision != "unavailable" || len(res.Report.Limitations) == 0 || !strings.Contains(res.Report.Limitations[0], "no vision model is configured") {
		t.Fatalf("%+v", res.Report)
	}
	// An enabled but never-tested model is not trusted with images either.
	var calls int32
	srv := visionServer(t, &calls, chartReply)
	defer srv.Close()
	e2 := visionEngine(t, srv.URL)
	c := e2.vision.config()
	c.VerifiedFingerprint = ""
	e2.vision.set(c)
	if _, err := e2.readPDF(context.Background(), data); err != nil || atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("unverified model got %d calls", calls)
	}
}

func TestVisionFailureAndLimitsStayExplicit(t *testing.T) {
	var calls int32
	srv := visionServer(t, &calls, "I see a chart but will not give JSON")
	defer srv.Close()
	e := visionEngine(t, srv.URL)
	j := testJPEG(300, 300)
	data := pdfWithPages([]testPage{{text: longText}, {jpeg: j, w: 300, h: 300}})
	res, err := e.readPDF(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Pages[1].Vision != "failed" || res.Report.VisionStatus != "failed" || len(res.Evidence) != 0 {
		t.Fatalf("%+v", res.Report)
	}
	if !strings.Contains(strings.Join(res.Report.Limitations, " "), "page 2: vision extraction failed") {
		t.Fatalf("%v", res.Report.Limitations)
	}

	// Per-document page cap: three scanned pages, limit of one.
	var calls2 int32
	srv2 := visionServer(t, &calls2, chartReply)
	defer srv2.Close()
	e2 := visionEngine(t, srv2.URL)
	c := e2.vision.config()
	c.Limits.MaxPagesPerDocument = 1
	e2.vision.set(c)
	three := pdfWithPages([]testPage{{jpeg: j, w: 300, h: 300}, {jpeg: testJPEG(301, 300), w: 301, h: 300}, {jpeg: testJPEG(302, 300), w: 302, h: 300}})
	res, _ = e2.readPDF(context.Background(), three)
	if atomic.LoadInt32(&calls2) != 1 || res.Report.Pages[1].Vision != "skipped" || res.Report.Pages[2].Vision != "skipped" {
		t.Fatalf("calls=%d %+v", calls2, res.Report.Pages)
	}
}

func TestRunBudgetIsSharedAcrossDocumentsInARun(t *testing.T) {
	var calls int32
	srv := visionServer(t, &calls, chartReply)
	defer srv.Close()
	e := visionEngine(t, srv.URL)
	c := e.vision.config()
	c.Limits.MaxImagesPerRun = 1
	e.vision.set(c)
	scope := ai.WithRunScope(context.Background())
	ctx := scope
	a := pdfWithPages([]testPage{{jpeg: testJPEG(300, 300), w: 300, h: 300}})
	b := pdfWithPages([]testPage{{jpeg: testJPEG(310, 300), w: 310, h: 300}})
	if r, _ := e.readPDF(ctx, a); r.Report.Pages[0].Vision != "ok" {
		t.Fatalf("%+v", r.Report)
	}
	r, _ := e.readPDF(ctx, b)
	if r.Report.Pages[0].Vision != "failed" || atomic.LoadInt32(&calls) != 1 || !strings.Contains(r.Report.Pages[0].Note, "budget") {
		t.Fatalf("calls=%d %+v", calls, r.Report.Pages)
	}
}

func TestFindPDFLinksStaysOnTheSiteAndRanksReports(t *testing.T) {
	page := `<html><body>
	<a href="/docs/privacy.pdf">Privacy</a>
	<a href="/files/Q3-2024-interim-report.pdf">Interim report Q3</a>
	<a href="https://evil.example.net/x.pdf">other site</a>
	<iframe src="/pdfjs/viewer.html?file=%2Ffiles%2Fannual-report-2023.pdf"></iframe>
	<embed src="/stream/42" type="application/pdf">
	<a href="javascript:void(0)">x</a><a href="/page.html">page</a>
	</body></html>`
	got := findPDFLinks(page, "https://ir.example.com/reports/")
	if len(got) < 3 || got[0] != "https://ir.example.com/files/Q3-2024-interim-report.pdf" && got[0] != "https://ir.example.com/files/annual-report-2023.pdf" {
		t.Fatalf("%v", got)
	}
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "evil.example.net") || !strings.Contains(joined, "/stream/42") || !strings.Contains(joined, "annual-report-2023.pdf") {
		t.Fatalf("%v", got)
	}
	if !looksLikeViewer("https://ir.example.com/pdfjs/viewer.html?file=a.pdf") || looksLikeViewer("https://ir.example.com/news") {
		t.Error("viewer detection")
	}
	if !sameSite("ir.example.com", "www.example.com") || sameSite("example.com", "example.org") || !sameSite("a.example.co.uk", "b.example.co.uk") || sameSite("a.co.uk", "b.co.uk") {
		t.Error("same-site rules")
	}
}

func TestReadPageStatusKeeps403AsDenial(t *testing.T) {
	err := readPageStatusError(403, mustURL("https://ir.example.com/report.pdf"))
	if err == nil || !strings.Contains(err.Error(), "denying automated page access") || !strings.Contains(err.Error(), "do not treat") {
		t.Fatalf("%v", err)
	}
}

func TestMagicBytes(t *testing.T) {
	if !hasPDFMagic([]byte("%PDF-1.7\n...")) || hasPDFMagic([]byte("<html>Access denied</html>")) {
		t.Error("magic bytes")
	}
}
