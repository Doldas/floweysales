package server

import (
	"context"
	"strings"
	"testing"
)

func TestReadPublicPageRefusesInternalAddresses(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/", "http://localhost/admin", "http://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/", "https://user:pw@example.com/", "ftp://example.com/", "http://example.com:8080/",
	} {
		if _, _, err := readPublicPage(context.Background(), raw); err == nil {
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
