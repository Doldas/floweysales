// Package sec reads public SEC EDGAR metadata and XBRL with explicit provenance.
// It does not synthesize company quarters from cumulative filings.
package sec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const officialDataURL = "https://data.sec.gov"
const officialArchiveURL = "https://www.sec.gov"
const officialTickerURL = "https://www.sec.gov/files/company_tickers_exchange.json"

// Client has a configurable transport for hermetic tests, but production URLs
// are fixed to SEC hosts. SEC_USER_AGENT must identify an actual contact.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	DataURL   string
	TickerURL string
	// ArchiveURL is the host of filing folders and their documents.
	ArchiveURL    string
	mu            sync.Mutex
	last          time.Time
	tickers       map[string]Company
	tickerExpires time.Time
}
type Company struct {
	CIK      int    `json:"cik"`
	Name     string `json:"name"`
	Ticker   string `json:"ticker"`
	Exchange string `json:"exchange"`
}
type Filing struct {
	Form        string `json:"form"`
	Accession   string `json:"accession"`
	Filed       string `json:"filed"`
	PeriodEnd   string `json:"period_end,omitempty"`
	DocumentURL string `json:"document_url"`
}
type Filings struct {
	Company     Company   `json:"company"`
	CIK         string    `json:"cik"`
	Source      string    `json:"source"`
	RetrievedAt time.Time `json:"retrieved_at"`
	Items       []Filing  `json:"filings"`
	Note        string    `json:"note"`
}
type Fact struct {
	Concept      string  `json:"concept"`
	Metric       string  `json:"metric"`
	Value        float64 `json:"value"`
	Unit         string  `json:"unit"`
	Start        string  `json:"start,omitempty"`
	End          string  `json:"end"`
	Filed        string  `json:"filed"`
	Form         string  `json:"form"`
	Accession    string  `json:"accession"`
	FiscalYear   int     `json:"fiscal_year"`
	FiscalPeriod string  `json:"fiscal_period"`
	Frame        string  `json:"frame,omitempty"`
	FilingURL    string  `json:"filing_url"`
	PeriodKind   string  `json:"period_kind"`
}
type Facts struct {
	Company     Company   `json:"company"`
	CIK         string    `json:"cik"`
	Source      string    `json:"source"`
	RetrievedAt time.Time `json:"retrieved_at"`
	Values      []Fact    `json:"facts"`
	Note        string    `json:"note"`
}

func NewClient(userAgent string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 14 * time.Second},
		UserAgent: strings.TrimSpace(userAgent), DataURL: officialDataURL, TickerURL: officialTickerURL, ArchiveURL: officialArchiveURL,
	}
}

var production = NewClient(os.Getenv("SEC_USER_AGENT"))

func Production() *Client { return production }
func (c *Client) get(ctx context.Context, endpoint string, limit int, into any) error {
	raw, err := c.getRaw(ctx, endpoint, limit, "application/json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("decode SEC response: %w", err)
	}
	return nil
}

// getRaw fetches one SEC document within the rate limit and size bound.
func (c *Client) getRaw(ctx context.Context, endpoint string, limit int, accept string) ([]byte, error) {
	if c.UserAgent == "" || !strings.Contains(c.UserAgent, "@") {
		return nil, errors.New("SEC_USER_AGENT must identify a real contact email for SEC fair access")
	}
	// Global per-client 2 requests/s maximum, safely under SEC's 10 req/s guidance.
	c.mu.Lock()
	delay := 500*time.Millisecond - time.Since(c.last)
	if delay > 0 {
		timer := time.NewTimer(delay)
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		c.mu.Lock()
	}
	c.last = time.Now()
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", accept)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("SEC EDGAR status %d (retry later or check User-Agent)", resp.StatusCode)
	}
	if resp.ContentLength > int64(limit) {
		return nil, errors.New("SEC response exceeds allowed size")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, errors.New("SEC response exceeds allowed size")
	}
	return raw, nil
}
func (c *Client) Resolve(ctx context.Context, ticker string) (Company, error) {
	key := strings.ToUpper(strings.TrimSpace(ticker))
	if key == "" || len(key) > 24 {
		return Company{}, errors.New("invalid ticker")
	}
	c.mu.Lock()
	if time.Now().Before(c.tickerExpires) {
		found, ok := c.tickers[key]
		c.mu.Unlock()
		if !ok {
			return Company{}, fmt.Errorf("SEC has no company mapping for %s", key)
		}
		return found, nil
	}
	c.mu.Unlock()
	var raw struct {
		Fields []string `json:"fields"`
		Data   [][]any  `json:"data"`
	}
	if err := c.get(ctx, c.TickerURL, 8*1024*1024, &raw); err != nil {
		return Company{}, err
	}
	index := map[string]int{}
	for i, name := range raw.Fields {
		index[strings.ToLower(name)] = i
	}
	for _, f := range []string{"cik", "name", "ticker", "exchange"} {
		if _, ok := index[f]; !ok {
			return Company{}, fmt.Errorf("SEC ticker mapping missing %s", f)
		}
	}
	entries := map[string]Company{}
	read := func(row []any, field string) any {
		at := index[field]
		if at >= len(row) {
			return nil
		}
		return row[at]
	}
	for _, row := range raw.Data {
		num, ok := read(row, "cik").(float64)
		if !ok || num <= 0 {
			continue
		}
		symbol, ok := read(row, "ticker").(string)
		if !ok {
			continue
		}
		name, _ := read(row, "name").(string)
		exchange, _ := read(row, "exchange").(string)
		normalized := strings.ToUpper(strings.TrimSpace(symbol))
		entries[normalized] = Company{CIK: int(num), Ticker: normalized, Name: name, Exchange: exchange}
	}
	c.mu.Lock()
	c.tickers = entries
	c.tickerExpires = time.Now().Add(24 * time.Hour)
	company, ok := entries[key]
	c.mu.Unlock()
	if !ok {
		return Company{}, fmt.Errorf("SEC has no company mapping for %s", key)
	}
	return company, nil
}
func (c *Client) Filings(ctx context.Context, ticker string, limit int) (Filings, error) {
	company, err := c.Resolve(ctx, ticker)
	if err != nil {
		return Filings{}, err
	}
	if limit < 1 {
		limit = 8
	}
	if limit > 30 {
		limit = 30
	}
	cik := fmt.Sprintf("%010d", company.CIK)
	var parsed struct {
		Filings struct {
			Recent struct {
				Accession []string `json:"accessionNumber"`
				Form      []string `json:"form"`
				Filed     []string `json:"filingDate"`
				Report    []string `json:"reportDate"`
				Primary   []string `json:"primaryDocument"`
			} `json:"recent"`
		} `json:"filings"`
	}
	if err := c.get(ctx, c.DataURL+"/submissions/CIK"+cik+".json", 12*1024*1024, &parsed); err != nil {
		return Filings{}, err
	}
	out := Filings{Company: company, CIK: cik, Source: "SEC EDGAR submissions", RetrievedAt: time.Now().UTC(), Items: []Filing{}, Note: "Official SEC filing links and dates; filings may be 10-K, 10-Q, 8-K or amendments."}
	rows := parsed.Filings.Recent
	for i, form := range rows.Form {
		if form != "10-K" && form != "10-Q" && form != "8-K" && form != "10-K/A" && form != "10-Q/A" && form != "8-K/A" && form != "20-F" && form != "40-F" {
			continue
		}
		if i >= len(rows.Accession) || i >= len(rows.Filed) || i >= len(rows.Primary) {
			continue
		}
		accession := rows.Accession[i]
		doc := rows.Primary[i]
		if !safeSECPart(doc) || !validAccession(accession) {
			continue
		}
		report := ""
		if i < len(rows.Report) {
			report = rows.Report[i]
		}
		link := filingLink(company.CIK, accession) + url.PathEscape(doc)
		out.Items = append(out.Items, Filing{Form: form, Accession: accession, Filed: rows.Filed[i], PeriodEnd: report, DocumentURL: link})
		if len(out.Items) >= limit {
			break
		}
	}
	return out, nil
}
func safeSECPart(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\?&#") && len(s) < 200
}
func validAccession(s string) bool {
	if len(s) != 20 {
		return false
	}
	for i, c := range s {
		if i == 10 || i == 13 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func filingLink(cik int, accession string) string {
	return "https://www.sec.gov/Archives/edgar/data/" + strconv.Itoa(cik) + "/" + strings.ReplaceAll(accession, "-", "") + "/"
}

var financialTags = map[string][]string{
	"revenue":             {"RevenueFromContractWithCustomerExcludingAssessedTax", "Revenues", "SalesRevenueNet"},
	"gross_profit":        {"GrossProfit"},
	"operating_income":    {"OperatingIncomeLoss"},
	"net_income":          {"NetIncomeLoss"},
	"operating_cash_flow": {"NetCashProvidedByUsedInOperatingActivities"},
	"capital_expenditure": {"PaymentsToAcquirePropertyPlantAndEquipment"},
	"assets":              {"Assets"},
	"liabilities":         {"Liabilities"},
	"cash":                {"CashAndCashEquivalentsAtCarryingValue"},
	"diluted_eps":         {"EarningsPerShareDiluted"},
	"shares_diluted":      {"WeightedAverageNumberOfDilutedSharesOutstanding"},
}

func (c *Client) CompanyFacts(ctx context.Context, ticker string) (Facts, error) {
	company, err := c.Resolve(ctx, ticker)
	if err != nil {
		return Facts{}, err
	}
	cik := fmt.Sprintf("%010d", company.CIK)
	var raw struct {
		Facts map[string]map[string]struct {
			Units map[string][]struct {
				Start     string  `json:"start"`
				End       string  `json:"end"`
				Value     float64 `json:"val"`
				Accession string  `json:"accn"`
				FY        int     `json:"fy"`
				FP        string  `json:"fp"`
				Form      string  `json:"form"`
				Filed     string  `json:"filed"`
				Frame     string  `json:"frame"`
			} `json:"units"`
		} `json:"facts"`
	}
	if err := c.get(ctx, c.DataURL+"/api/xbrl/companyfacts/CIK"+cik+".json", 32*1024*1024, &raw); err != nil {
		return Facts{}, err
	}
	out := Facts{Company: company, CIK: cik, Source: "SEC EDGAR XBRL companyfacts", RetrievedAt: time.Now().UTC(), Values: []Fact{}, Note: "Values are filing-sourced; durations distinguish quarterly, annual and instantaneous figures. Do not sum year-to-date and quarterly values."}
	tags := raw.Facts["us-gaap"]
	for metric, candidates := range financialTags {
		for _, tag := range candidates {
			values, ok := tags[tag]
			if !ok {
				continue
			}
			for unit, observations := range values.Units {
				if unit != "USD" && unit != "USD/shares" && unit != "shares" {
					continue
				}
				latest := map[string]Fact{}
				for _, v := range observations {
					if v.Form != "10-Q" && v.Form != "10-K" {
						continue
					}
					if !validAccession(v.Accession) || v.End == "" {
						continue
					}
					kind := "instant"
					if v.Start != "" {
						start, e1 := time.Parse("2006-01-02", v.Start)
						end, e2 := time.Parse("2006-01-02", v.End)
						if e1 != nil || e2 != nil {
							continue
						}
						days := int(end.Sub(start).Hours()/24) + 1
						switch {
						case days >= 75 && days <= 110:
							kind = "quarter"
						case days >= 330 && days <= 390:
							kind = "annual"
						default:
							continue // explicitly exclude YTD / nine-month duration
						}
					}
					key := kind + "|" + v.End + "|" + unit
					fact := Fact{Concept: "us-gaap:" + tag, Metric: metric, Value: v.Value, Unit: unit,
						Start: v.Start, End: v.End, Filed: v.Filed, Form: v.Form, Accession: v.Accession,
						FiscalYear: v.FY, FiscalPeriod: v.FP, Frame: v.Frame, PeriodKind: kind,
						FilingURL: filingLink(company.CIK, v.Accession)}
					old, ok := latest[key]
					if !ok || fact.Filed > old.Filed {
						latest[key] = fact
					}
				}
				for _, fact := range latest {
					out.Values = append(out.Values, fact)
				}
			}
			break // first available matching GAAP concept for this metric
		}
	}
	sort.Slice(out.Values, func(i, j int) bool {
		a, b := out.Values[i], out.Values[j]
		if a.End != b.End {
			return a.End > b.End
		}
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		return a.PeriodKind < b.PeriodKind
	})
	if len(out.Values) > 80 {
		out.Values = out.Values[:80]
	}
	return out, nil
}
