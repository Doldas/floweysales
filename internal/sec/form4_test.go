package sec

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseForm4KeepsOnlyOpenMarketTrades(t *testing.T) {
	buys, err := ParseForm4(readFixture(t, "form4-buy.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(buys) != 2 {
		t.Fatalf("a grant (A) is not a purchase: %+v", buys)
	}
	b := buys[0]
	if b.Owner != "Doe Jane" || !b.IsDirector || !b.IsOfficer || b.Title != "Chief Executive Officer" || b.Code != "P" || b.Date != "2026-10-20" || b.Shares != 10000 {
		t.Fatalf("%+v", b)
	}
	if b.Price == nil || *b.Price != 25.5 || b.Value == nil || *b.Value != 255000 {
		t.Fatalf("price and value: %+v", b)
	}
	if buys[1].Price != nil || buys[1].Value != nil {
		t.Fatalf("a missing price stays absent, not zero: %+v", buys[1])
	}

	sales, err := ParseForm4(readFixture(t, "form4-sale.xml"))
	if err != nil || len(sales) != 1 || sales[0].Code != "S" || sales[0].IsDirector || !sales[0].IsOfficer || *sales[0].Value != 105000 {
		t.Fatalf("%+v err=%v", sales, err)
	}
	if _, err := ParseForm4([]byte("not xml <")); err == nil {
		t.Fatal("garbage is an error")
	}
	if none, err := ParseForm4([]byte(`<ownershipDocument></ownershipDocument>`)); err != nil || len(none) != 0 {
		t.Fatalf("%+v %v", none, err)
	}
}

func TestInsiderTradesFollowsIndexAndLookback(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	old := time.Now().UTC().AddDate(0, 0, -200).Format("2006-01-02")
	buy, sale := readFixture(t, "form4-buy.xml"), readFixture(t, "form4-sale.xml")
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "company_tickers"):
			fmt.Fprint(w, `{"fields":["cik","name","ticker","exchange"],"data":[[1045810,"EXAMPLE CORP","EXMP","Nasdaq"]]}`)
		case strings.Contains(r.URL.Path, "submissions"):
			fmt.Fprintf(w, `{"filings":{"recent":{"accessionNumber":["0001000001-26-000001","0001000002-26-000002","0001000003-26-000003","0001000004-24-000004"],"form":["4","8-K","4","4"],"filingDate":["%s","%s","%s","%s"]}}}`, recent, recent, recent, old)
		case strings.HasSuffix(r.URL.Path, "/index.json"):
			fmt.Fprint(w, `{"directory":{"item":[{"name":"FilingSummary.xml"},{"name":"form4.xml"},{"name":"primary_doc.html"}]}}`)
		case strings.Contains(r.URL.Path, "000100000100000001") || strings.Contains(r.URL.Path, "000100000126000001"):
			w.Write(buy)
		case strings.Contains(r.URL.Path, "000100000326000003"):
			w.Write(sale)
		default:
			http.Error(w, "missing", 404)
		}
	}))
	defer srv.Close()
	c := NewClient("FloweySales test@example.com")
	c.DataURL, c.ArchiveURL, c.TickerURL, c.HTTP = srv.URL, srv.URL, srv.URL+"/company_tickers_exchange.json", srv.Client()
	c.last = time.Now().Add(-time.Hour)

	trades, err := c.InsiderTrades(context.Background(), "exmp", 30, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 3 {
		t.Fatalf("two Form 4s in the window give three open-market trades; the 8-K and the old filing are skipped: %+v", trades)
	}
	if trades[0].Date < trades[len(trades)-1].Date {
		t.Fatalf("newest first: %+v", trades)
	}
	for _, tr := range trades {
		if tr.Filed != recent || !strings.Contains(tr.URL, "/Archives/edgar/data/1045810/") {
			t.Fatalf("provenance: %+v", tr)
		}
	}
	for _, p := range paths {
		if strings.Contains(p, "000100000424000004") {
			t.Fatalf("the filing outside the lookback must not be fetched: %v", paths)
		}
	}
}
