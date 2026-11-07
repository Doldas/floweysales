package server

import (
	"fmt"
	"strings"
	"testing"
)

// infoPDF builds a one-page PDF with an information dictionary.
func infoPDF(info string) []byte {
	var b strings.Builder
	offsets := []int{}
	add := func(s string) { offsets = append(offsets, b.Len()); b.WriteString(s) }
	b.WriteString("%PDF-1.4\n")
	add("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	add("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	add("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>\nendobj\n")
	add("4 0 obj\n<< " + info + " >>\nendobj\n")
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 5\n0000000000 65535 f \n")
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\nstartxref\n%d\n%%%%EOF\n", x)
	return []byte(b.String())
}

func TestPDFInfoDatesAreFileDatesOnly(t *testing.T) {
	created, modified := pdfInfoDates(infoPDF("/CreationDate (D:20261001120000+02'00') /ModDate (D:20261002080000Z)"))
	if created != "2026-10-01T12:00:00+02:00" || modified != "2026-10-02T08:00:00Z" {
		t.Fatalf("%q %q", created, modified)
	}
	if c, m := pdfInfoDates(infoPDF("/Title (x)")); c != "" || m != "" {
		t.Fatal("missing dates must stay empty")
	}
	if c, m := pdfInfoDates([]byte("not a pdf")); c != "" || m != "" {
		t.Fatal("garbage must yield nothing")
	}
}
