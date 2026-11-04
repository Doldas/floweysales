// Package universecheck decides whether a market can be offered on the
// leaderboard (issue #209). It holds the candidate markets, the bar a market
// must clear and the evaluation of a leaderboard response against that bar.
//
// Nothing here talks to a provider. The measurements come from a response of
// GET /api/market/leaderboard, which a maintainer with provider access fetches
// (cmd/universe-coverage does it) or saves and evaluates later. The candidate
// symbols are names to test, not a claim that they resolve: a market is
// supported only after a run says so and a list is then added to universes.json.
package universecheck

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"floweysales/internal/market"
)

// The bar, set before looking at results (Plan 4, WP3 step 1): at least 80% of
// a market's candidates need a year of prices and four reported quarters for
// full ranking.
const (
	BarShare = 0.80
	// StaleDays is the age of the newest price, in calendar days, beyond which a
	// market's data is reported as old rather than current.
	StaleDays = 7
	// MinCandidates is the fewest candidates a verdict rests on.
	MinCandidates = 5
)

// Verdicts.
const (
	Full        = "full"        // prices and reports clear the bar
	PricesOnly  = "prices_only" // prices clear it, reports do not: low confidence only
	Unqualified = "unqualified" // prices do not clear it
	NoSymbols   = "no_symbols"  // no candidates were supplied, nothing can be said
	TooFew      = "too_few"     // fewer candidates than a verdict needs
	NoData      = "no_data"     // no response for the market
)

// Market is a candidate market: where it trades and the symbols to try.
type Market struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Region  string   `json:"region"`
	Country string   `json:"country"`
	Suffix  string   `json:"suffix"`  // the provider's ticker suffix, without the dot
	Quoted  []string `json:"quoted"`  // currencies a price may be quoted in
	Symbols []string `json:"symbols"` // candidates to test, one list per market
	Note    string   `json:"note,omitempty"`
}

// Candidates are the markets named in Plan 4 WP3. The symbols are widely held
// large companies chosen from memory, not checked against any provider; the
// run is what checks them. Lagos, Nairobi and Cairo have no symbols: the
// provider's ticker format for them is not known here, and a made-up suffix
// would only produce a misleading "no data". A maintainer who learns the real
// format adds them here.
func Candidates() []Market {
	return []Market{
		{ID: "tokyo", Label: "Tokyo", Region: "asia", Country: "JP", Suffix: "T", Quoted: []string{"JPY"},
			Symbols: []string{"7203.T", "6758.T", "9984.T", "8306.T", "6861.T", "9432.T", "8035.T", "6501.T"}},
		{ID: "hong_kong", Label: "Hong Kong", Region: "asia", Country: "HK", Suffix: "HK", Quoted: []string{"HKD"},
			Symbols: []string{"0700.HK", "9988.HK", "0005.HK", "1299.HK", "0941.HK", "3690.HK", "0388.HK", "2318.HK"}},
		{ID: "india", Label: "India (NSE)", Region: "asia", Country: "IN", Suffix: "NS", Quoted: []string{"INR"},
			Symbols: []string{"RELIANCE.NS", "TCS.NS", "HDFCBANK.NS", "INFY.NS", "ICICIBANK.NS", "ITC.NS", "LT.NS", "SBIN.NS"}},
		{ID: "korea", Label: "Korea", Region: "asia", Country: "KR", Suffix: "KS", Quoted: []string{"KRW"},
			Symbols: []string{"005930.KS", "000660.KS", "035420.KS", "005380.KS", "051910.KS", "035720.KS", "207940.KS", "068270.KS"}},
		{ID: "johannesburg", Label: "Johannesburg", Region: "africa", Country: "ZA", Suffix: "JO", Quoted: []string{"ZAR", "ZAc"},
			Symbols: []string{"NPN.JO", "SOL.JO", "FSR.JO", "SBK.JO", "AGL.JO", "MTN.JO", "CPI.JO", "BTI.JO"},
			Note:    "Prices on this exchange may be quoted in cents (ZAc); the run reports the currency it finds."},
		{ID: "lagos", Label: "Lagos", Region: "africa", Country: "NG", Note: "The provider's ticker format for Nigerian stocks is not known; supply symbols and a suffix after checking."},
		{ID: "nairobi", Label: "Nairobi", Region: "africa", Country: "KE", Note: "The provider's ticker format for Kenyan stocks is not known; supply symbols and a suffix after checking."},
		{ID: "cairo", Label: "Cairo", Region: "africa", Country: "EG", Note: "The provider's ticker format for Egyptian stocks is not known; supply symbols and a suffix after checking."},
		{ID: "sao_paulo", Label: "Sao Paulo", Region: "south_america", Country: "BR", Suffix: "SA", Quoted: []string{"BRL"},
			Symbols: []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA", "BBDC4.SA", "ABEV3.SA", "WEGE3.SA", "B3SA3.SA", "BBAS3.SA"}},
		{ID: "sydney", Label: "Sydney", Region: "oceania", Country: "AU", Suffix: "AX", Quoted: []string{"AUD"},
			Symbols: []string{"BHP.AX", "CBA.AX", "CSL.AX", "NAB.AX", "WBC.AX", "ANZ.AX", "WES.AX", "MQG.AX"}},
		{ID: "toronto", Label: "Toronto", Region: "north_america", Country: "CA", Suffix: "TO", Quoted: []string{"CAD"},
			Symbols: []string{"RY.TO", "TD.TO", "SHOP.TO", "ENB.TO", "CNQ.TO", "BNS.TO", "CP.TO", "BMO.TO"}},
	}
}

// Validate checks a candidate definition without any network: symbols are well
// formed, unique and carry the market's suffix, and the market says which
// currency to expect. It returns every problem, not the first.
func Validate(m Market) []string {
	var problems []string
	if m.ID == "" || m.Label == "" || m.Region == "" || m.Country == "" {
		problems = append(problems, "id, label, region and country are required")
	}
	if len(m.Symbols) == 0 {
		return problems // a market with no candidates is allowed; the run says "no symbols"
	}
	if m.Suffix == "" {
		problems = append(problems, "symbols given without a suffix")
	}
	if len(m.Quoted) == 0 {
		problems = append(problems, "no expected currency")
	}
	seen := map[string]bool{}
	for _, s := range m.Symbols {
		canonical, err := market.CanonicalSymbol(s)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("symbol %q: %v", s, err))
		case canonical != s:
			problems = append(problems, fmt.Sprintf("symbol %q is not in canonical form (%s)", s, canonical))
		case seen[s]:
			problems = append(problems, fmt.Sprintf("symbol %q is listed twice", s))
		case !strings.HasSuffix(s, "."+m.Suffix):
			problems = append(problems, fmt.Sprintf("symbol %q does not end in .%s", s, m.Suffix))
		}
		seen[s] = true
	}
	if len(m.Symbols) > 25 {
		problems = append(problems, "a leaderboard request takes at most 25 symbols")
	}
	return problems
}

// Response is the part of a leaderboard response the check reads.
type Response struct {
	Provider string `json:"provider"`
	AsOf     string `json:"as_of"`
	Rows     []struct {
		Symbol     string   `json:"symbol"`
		Currency   string   `json:"currency"`
		AsOf       string   `json:"as_of"`
		Change1Y   *float64 `json:"change_1y_pct"`
		TTMThrough string   `json:"ttm_through"`
	} `json:"rows"`
	Unranked []struct {
		Symbol string `json:"symbol"`
		Error  string `json:"error"`
	} `json:"unranked"`
}

// ParseResponse reads a saved leaderboard response.
func ParseResponse(raw []byte) (Response, error) {
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("not a leaderboard response: %w", err)
	}
	return r, nil
}

// SymbolResult is what the run found for one candidate.
type SymbolResult struct {
	Symbol   string `json:"symbol"`
	Priced   bool   `json:"priced"`
	Year     bool   `json:"year_of_prices"`
	Reports  bool   `json:"four_quarters"`
	Currency string `json:"currency,omitempty"`
	PriceDay string `json:"price_date,omitempty"`
	Problem  string `json:"problem,omitempty"`
}

// Result is the verdict for one market.
type Result struct {
	Market         string         `json:"market"`
	Label          string         `json:"label"`
	Region         string         `json:"region"`
	Candidates     int            `json:"candidates"`
	PricedPct      float64        `json:"priced_pct"`
	YearPct        float64        `json:"year_of_prices_pct"`
	ReportsPct     float64        `json:"four_quarters_pct"`
	Currencies     []string       `json:"currencies"`
	NewestPrice    string         `json:"newest_price,omitempty"`
	OldestPrice    string         `json:"oldest_price,omitempty"`
	Verdict        string         `json:"verdict"`
	Findings       []string       `json:"findings"`
	Symbols        []SymbolResult `json:"symbols"`
	Provider       string         `json:"provider,omitempty"`
	EnablesNothing string         `json:"note"`
}

const enablesNothing = "A verdict is a measurement of one run. Adding a universe still means writing a reviewed list into internal/server/universes.json."

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(int(float64(n)/float64(d)*1000+0.5)) / 10
}

// Evaluate measures one market against the bar from a leaderboard response
// fetched for exactly its candidate symbols. now is the day the run is
// judged on, so data age can be stated; pass the run's own date.
func Evaluate(m Market, resp *Response, now time.Time) Result {
	res := Result{Market: m.ID, Label: m.Label, Region: m.Region, Candidates: len(m.Symbols), Currencies: []string{}, Findings: []string{}, Symbols: []SymbolResult{}, EnablesNothing: enablesNothing}
	switch {
	case len(m.Symbols) == 0:
		res.Verdict = NoSymbols
		res.Findings = append(res.Findings, "No candidate symbols: "+m.Note)
		return res
	case resp == nil:
		res.Verdict = NoData
		res.Findings = append(res.Findings, "No response was supplied for this market, so nothing is claimed about it.")
		return res
	}
	res.Provider = resp.Provider
	byRow := map[string]int{}
	for i, r := range resp.Rows {
		byRow[r.Symbol] = i
	}
	failed := map[string]string{}
	for _, u := range resp.Unranked {
		failed[u.Symbol] = u.Error
	}
	priced, year, reports := 0, 0, 0
	currencies := map[string]bool{}
	for _, s := range m.Symbols {
		sr := SymbolResult{Symbol: s}
		if i, ok := byRow[s]; ok {
			r := resp.Rows[i]
			sr.Priced, sr.Year, sr.Reports = true, r.Change1Y != nil, r.TTMThrough != ""
			sr.Currency, sr.PriceDay = r.Currency, r.AsOf
			if sr.Currency != "" {
				currencies[sr.Currency] = true
			}
			if sr.PriceDay != "" {
				if res.NewestPrice == "" || sr.PriceDay > res.NewestPrice {
					res.NewestPrice = sr.PriceDay
				}
				if res.OldestPrice == "" || sr.PriceDay < res.OldestPrice {
					res.OldestPrice = sr.PriceDay
				}
			}
			switch {
			case !sr.Year:
				sr.Problem = "less than a year of prices"
			case !sr.Reports:
				sr.Problem = "fewer than four reported quarters"
			}
		} else if msg, ok := failed[s]; ok {
			sr.Problem = msg
		} else {
			sr.Problem = "absent from the response"
		}
		if sr.Priced {
			priced++
		}
		if sr.Year {
			year++
		}
		if sr.Year && sr.Reports {
			reports++
		}
		res.Symbols = append(res.Symbols, sr)
	}
	n := len(m.Symbols)
	res.PricedPct, res.YearPct, res.ReportsPct = pct(priced, n), pct(year, n), pct(reports, n)
	for c := range currencies {
		res.Currencies = append(res.Currencies, c)
	}
	sort.Strings(res.Currencies)

	expected := map[string]bool{}
	for _, c := range m.Quoted {
		expected[c] = true
	}
	for _, c := range res.Currencies {
		if !expected[c] {
			res.Findings = append(res.Findings, fmt.Sprintf("Currency %s is not one of the expected %s; comparing this market with others needs care.", c, strings.Join(m.Quoted, "/")))
		}
		if c == "ZAc" || c == "GBp" || c == "GBX" {
			res.Findings = append(res.Findings, fmt.Sprintf("Prices are quoted in %s (a hundredth of the main unit); per-share figures must not be mixed with the main currency.", c))
		}
	}
	if t, err := time.Parse("2006-01-02", res.NewestPrice); err == nil {
		if age := int(now.UTC().Sub(t).Hours() / 24); age > StaleDays {
			res.Findings = append(res.Findings, fmt.Sprintf("The newest price is %d days old, so the data for this market is stale.", age))
		}
	}
	if res.NewestPrice != res.OldestPrice {
		res.Findings = append(res.Findings, fmt.Sprintf("Price dates run from %s to %s.", res.OldestPrice, res.NewestPrice))
	}

	switch {
	case n < MinCandidates:
		res.Verdict = TooFew
		res.Findings = append(res.Findings, fmt.Sprintf("Only %d candidates; a verdict needs at least %d.", n, MinCandidates))
	case float64(year)/float64(n) < BarShare:
		res.Verdict = Unqualified
		res.Findings = append(res.Findings, fmt.Sprintf("%.0f%% have a year of prices; the bar is %.0f%%.", res.YearPct, BarShare*100))
	case float64(reports)/float64(n) < BarShare:
		res.Verdict = PricesOnly
		res.Findings = append(res.Findings, fmt.Sprintf("%.0f%% have a year of prices but only %.0f%% have four reported quarters (bar %.0f%%): at best a prices-only ranking with low confidence.", res.YearPct, res.ReportsPct, BarShare*100))
	default:
		res.Verdict = Full
		res.Findings = append(res.Findings, fmt.Sprintf("%.0f%% have a year of prices and four reported quarters (bar %.0f%%).", res.ReportsPct, BarShare*100))
	}
	return res
}
