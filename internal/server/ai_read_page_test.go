package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestReadPublicPageRefusesInternalAddresses(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/", "http://localhost/admin", "http://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/", "https://user:pw@example.com/", "ftp://example.com/", "http://example.com:8080/",
	} {
		if _, _, _, err := readPublicPage(context.Background(), raw); err == nil {
			t.Errorf("%s must be refused", raw)
		}
	}
}

func TestHTMLTextDropsScriptsAndMarkup(t *testing.T) {
	got := htmlText(`<html><head><style>p{color:red}</style><script>alert(1)</script></head><body><p>Revenue &amp; profit</p> <b>rose</b></body></html>`)
	if got != "Revenue & profit rose" || strings.Contains(got, "alert") {
		t.Fatalf("got %q", got)
	}
}

// minimalPDF builds a one-page PDF with a text layer, computing the xref offsets.
func minimalPDF(text string) []byte {
	stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestPDFTextReadsATextLayerAndRejectsGarbage(t *testing.T) {
	got, err := pdfText(minimalPDF("Revenue was 4300 million"))
	if err != nil || !strings.Contains(got, "Revenue was 4300 million") {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range [][]byte{[]byte("not a pdf"), minimalPDF("x")[:40], nil} {
		if _, err := pdfText(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestReadPageStatusErrorExplainsSEC403WithoutClaimingToRead(t *testing.T) {
	tests := []struct {
		name, target string
		status       int
		want         string
	}{
		{"SEC", "https://www.sec.gov/Archives/edgar/data/1/example.htm", http.StatusForbidden, "get_sec_section"},
		{"SEC host suffix", "https://data.sec.gov/submissions/CIK0000000001.json", http.StatusForbidden, "get_sec_section"},
		{"Other site forbidden", "https://example.org/report", http.StatusForbidden, "automated page access"},
		{"Rate limited", "https://example.org/report", http.StatusTooManyRequests, "automated page access"},
		{"Other HTTP error", "https://example.org/report", http.StatusBadGateway, "HTTP status 502"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target, err := url.Parse(tc.target)
			if err != nil {
				t.Fatal(err)
			}
			message := readPageStatusError(tc.status, target).Error()
			if !strings.Contains(message, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, message)
			}
		})
	}
}

func TestDocumentFetchRefusesOversizeAndMislabeledPDF(t *testing.T) {
	tests := []struct {
		name string
		kind string
		body []byte
		limit int64
		want string
	}{
		{"valid PDF", "application/pdf", minimalPDF("Quarterly report"), readPDFMaxBytes, ""},
		{"mislabeled PDF", "application/pdf", []byte("<html>Access denied</html>"), readPDFMaxBytes, "no PDF signature"},
		{"huge HTML", "text/html", []byte(strings.Repeat("x", readPageMaxBytes+1)), readPageMaxBytes, "larger than"},
		{"generic binary", "application/octet-stream", []byte("not a pdf"), readPDFMaxBytes, "cannot read"},
		{"normal HTML", "text/html", []byte("<html>valid</html>"), readPageMaxBytes, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isPDF, err := validateReadPageBody(tc.kind, tc.body, tc.limit)
			if tc.want == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want %q, got pdf=%v error=%v", tc.want, isPDF, err)
			}
			if tc.name == "valid PDF" && !isPDF {
				t.Fatal("valid PDF signature lost")
			}
		})
	}
}
