package sec

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// InsiderTrade is one open-market purchase (P) or sale (S) of the issuer's
// stock reported on a Form 4. Price is absent when the filing leaves it out;
// Value is Shares × Price and absent with it.
type InsiderTrade struct {
	Owner      string   `json:"owner"`
	IsDirector bool     `json:"is_director"`
	IsOfficer  bool     `json:"is_officer"`
	Title      string   `json:"title,omitempty"`
	Date       string   `json:"date"`
	Code       string   `json:"code"` // P = open-market buy, S = sale
	Shares     float64  `json:"shares"`
	Price      *float64 `json:"price,omitempty"`
	Value      *float64 `json:"value,omitempty"`
	Filed      string   `json:"filed,omitempty"`
	URL        string   `json:"url,omitempty"`
}

type f4Value struct {
	Value string `xml:"value"`
}

type f4Owner struct {
	Name         string `xml:"reportingOwnerId>rptOwnerName"`
	Relationship struct {
		IsDirector string `xml:"isDirector"`
		IsOfficer  string `xml:"isOfficer"`
		Title      string `xml:"officerTitle"`
	} `xml:"reportingOwnerRelationship"`
}

type f4Document struct {
	Owners []f4Owner `xml:"reportingOwner"`
	Trades []struct {
		Date   f4Value `xml:"transactionDate"`
		Code   string  `xml:"transactionCoding>transactionCode"`
		Shares f4Value `xml:"transactionAmounts>transactionShares"`
		Price  f4Value `xml:"transactionAmounts>transactionPricePerShare"`
	} `xml:"nonDerivativeTable>nonDerivativeTransaction"`
}

func flagSet(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true"
}

func positive(v string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f, err == nil && f > 0 && !math.IsInf(f, 0)
}

// ParseForm4 reads the non-derivative transactions of a Form 4 and keeps the
// open-market purchases and sales; grants, exercises, gifts and other codes
// are ignored. The first reporting owner is the one named, as joint filers
// report the same trade.
func ParseForm4(raw []byte) ([]InsiderTrade, error) {
	var doc f4Document
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode Form 4: %w", err)
	}
	owner := f4Owner{}
	if len(doc.Owners) > 0 {
		owner = doc.Owners[0]
	}
	out := []InsiderTrade{}
	for _, t := range doc.Trades {
		code := strings.ToUpper(strings.TrimSpace(t.Code))
		if code != "P" && code != "S" {
			continue
		}
		shares, ok := positive(t.Shares.Value)
		date := strings.TrimSpace(t.Date.Value)
		if _, err := time.Parse("2006-01-02", date); !ok || err != nil {
			continue
		}
		trade := InsiderTrade{
			Owner: strings.TrimSpace(owner.Name), IsDirector: flagSet(owner.Relationship.IsDirector), IsOfficer: flagSet(owner.Relationship.IsOfficer),
			Title: strings.TrimSpace(owner.Relationship.Title), Date: date, Code: code, Shares: shares,
		}
		if price, ok := positive(t.Price.Value); ok {
			value := math.Round(shares*price*100) / 100
			trade.Price, trade.Value = &price, &value
		}
		out = append(out, trade)
	}
	return out, nil
}

// maxForm4Filings bounds the documents fetched for one company; each costs a
// rate-limited request.
const maxForm4Filings = 20

// InsiderTrades lists the open-market purchases and sales reported on the
// company's Form 4 filings of the last lookbackDays, newest first.
func (c *Client) InsiderTrades(ctx context.Context, ticker string, lookbackDays, maxFilings int) ([]InsiderTrade, error) {
	company, err := c.Resolve(ctx, ticker)
	if err != nil {
		return nil, err
	}
	if maxFilings < 1 || maxFilings > maxForm4Filings {
		maxFilings = maxForm4Filings
	}
	cik := fmt.Sprintf("%010d", company.CIK)
	var parsed struct {
		Filings struct {
			Recent struct {
				Accession []string `json:"accessionNumber"`
				Form      []string `json:"form"`
				Filed     []string `json:"filingDate"`
			} `json:"recent"`
		} `json:"filings"`
	}
	if err := c.get(ctx, c.DataURL+"/submissions/CIK"+cik+".json", 12*1024*1024, &parsed); err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -lookbackDays).Format("2006-01-02")
	rows := parsed.Filings.Recent
	trades := []InsiderTrade{}
	fetched := 0
	for i, form := range rows.Form {
		if form != "4" || i >= len(rows.Accession) || i >= len(rows.Filed) || !validAccession(rows.Accession[i]) {
			continue
		}
		if rows.Filed[i] < cutoff {
			break // submissions list newest first
		}
		if fetched >= maxFilings {
			break
		}
		fetched++
		folder := filingFolder(c.ArchiveURL, company.CIK, rows.Accession[i])
		name, err := c.form4Document(ctx, folder)
		if err != nil {
			continue // one unreadable filing does not hide the others
		}
		raw, err := c.getRaw(ctx, folder+url.PathEscape(name), 1024*1024, "application/xml")
		if err != nil {
			continue
		}
		found, err := ParseForm4(raw)
		if err != nil {
			continue
		}
		for _, t := range found {
			t.Filed, t.URL = rows.Filed[i], folder
			trades = append(trades, t)
		}
	}
	sort.SliceStable(trades, func(i, j int) bool { return trades[i].Date > trades[j].Date })
	return trades, nil
}

func filingFolder(base string, cik int, accession string) string {
	return strings.TrimRight(base, "/") + "/Archives/edgar/data/" + strconv.Itoa(cik) + "/" + strings.ReplaceAll(accession, "-", "") + "/"
}

// form4Document finds the raw XML of a filing in its folder index: the first
// .xml that is not the filing summary.
func (c *Client) form4Document(ctx context.Context, folder string) (string, error) {
	var index struct {
		Directory struct {
			Item []struct {
				Name string `json:"name"`
			} `json:"item"`
		} `json:"directory"`
	}
	if err := c.get(ctx, folder+"index.json", 256*1024, &index); err != nil {
		return "", err
	}
	for _, item := range index.Directory.Item {
		name := item.Name
		if strings.HasSuffix(strings.ToLower(name), ".xml") && !strings.HasPrefix(strings.ToLower(name), "filingsummary") && safeSECPart(name) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no Form 4 document in %s", folder)
}
