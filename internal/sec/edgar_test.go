package sec

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFilingsProvenanceAndNoGuessedFiscalQuarters(t *testing.T) {
	accession := "0001045810-26-000027"
	var urls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "test@example.com") {
			t.Error("contact missing")
		}
		urls = append(urls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "company_tickers"):
			fmt.Fprint(w, `{"fields":["cik","name","ticker","exchange"],"data":[[1045810,"NVIDIA CORP","NVDA","Nasdaq"] , [1000,"Other","OTHER","NYSE"]]}`)
		case strings.Contains(r.URL.Path, "submissions"):
			fmt.Fprintf(w, `{"filings":{"recent":{"accessionNumber":["%s"],"form":["10-Q"],"filingDate":["2026-08-26"],"reportDate":["2026-07-26"],"primaryDocument":["nvda-20260726.htm"]}}}`, accession)
		case strings.Contains(r.URL.Path, "companyfacts"):
			fmt.Fprintf(w, `{"facts":{"us-gaap":{"Revenues":{"units":{"USD":[{"start":"2026-04-27","end":"2026-07-26","val":96000,"accn":"%s","filed":"2026-08-26","form":"10-Q","fy":2027,"fp":"Q2"},{"start":"2026-01-26","end":"2026-07-26","val":170000,"accn":"%s","filed":"2026-08-26","form":"10-Q","fy":2027,"fp":"Q2"}]}}}}}`, accession, accession)
		default:
			http.Error(w, "missing", 404)
		}
	}))
	defer srv.Close()
	c := NewClient("FloweySales test@example.com")
	c.DataURL = srv.URL
	c.TickerURL = srv.URL + "/company_tickers_exchange.json"
	c.HTTP = srv.Client()
	filings, err := c.Filings(context.Background(), "nvda", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(filings.Items) != 1 || filings.Items[0].PeriodEnd != "2026-07-26" || !strings.Contains(filings.Items[0].DocumentURL, "/1045810/") {
		t.Fatalf("%+v", filings)
	}
	facts, err := c.CompanyFacts(context.Background(), "NVDA")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Values) != 1 || facts.Values[0].PeriodKind != "quarter" || facts.Values[0].FiscalPeriod != "Q2" || facts.Values[0].Value != 96000 {
		t.Fatalf("%+v", facts)
	}
	if len(urls) != 3 {
		t.Fatalf("expected cached ticker index (3 HTTP requests), got %d", len(urls))
	}
}
func TestSECRequiresContact(t *testing.T) {
	c := NewClient("")
	c.TickerURL = "https://example.test"
	if _, err := c.Resolve(context.Background(), "NVDA"); err == nil {
		t.Fatal("must reject unclassified bot")
	}
}
