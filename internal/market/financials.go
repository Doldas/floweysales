package market

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

// QuarterlyFinancial is one reported company quarter. Missing values are
// omitted so the frontend can render "—" instead of a misleading zero.
type QuarterlyFinancial struct {
	Period          string   `json:"period"`                 // Calendar quarter for Yahoo, fiscal quarter for Finnhub
	PeriodBasis     string   `json:"period_basis,omitempty"` // calendar | fiscal
	PeriodEnd       string   `json:"period_end"`             // "2026-06-30"
	Revenue         *float64 `json:"revenue,omitempty"`
	GrossProfit     *float64 `json:"gross_profit,omitempty"`
	OperatingIncome *float64 `json:"operating_income,omitempty"`
	NetIncome       *float64 `json:"net_income,omitempty"`
	EPS             *float64 `json:"eps,omitempty"`
	DataQualityWarnings []string `json:"data_quality_warnings,omitempty"`
	FreeCashFlow    *float64 `json:"free_cash_flow,omitempty"`
	Currency        string   `json:"currency,omitempty"`
	Form            string   `json:"form,omitempty"` // "10-Q" etc (SEC filings)
}

// FinancialsResponse is what GET /api/financials serves. Note is set when the
// provider/asset combination legitimately has no reports (e.g. crypto).
type FinancialsResponse struct {
	Symbol   string               `json:"symbol"`
	Provider string               `json:"provider"`
	Source   string               `json:"source"` // "yahoo" | "finnhub" | "none"
	Reports  []QuarterlyFinancial `json:"reports"`
	Note     string               `json:"note,omitempty"`
}

// FetchQuarterlyFinancials returns the most recent quarterly reports for a
// ticker, provider-dependent:
//   - Yahoo   → fundamentals-timeseries (last ~5 quarters)
//   - Avanza  → Yahoo via the market-guide ticker mapping
//   - Finnhub → financials-reported (last ~8 quarters)
//   - Binance → crypto has no financial statements
func FetchQuarterlyFinancials(symbol string, provider Provider, finnhubKey string) (FinancialsResponse, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return FinancialsResponse{}, fmt.Errorf("[Financials] empty symbol")
	}

	switch provider {
	case ProviderYahoo:
		return fetchYahooFinancials(symbol)
	case ProviderAvanza:
		// The market-guide endpoint only accepts numeric orderbook IDs, so a
		// Yahoo-style ticker (e.g. "VOL-B" tracked from the Yahoo provider)
		// must be resolved through the Avanza search first — same as history.
		id := symbol
		if !isDigits(symbol) {
			resolved, _, err := resolveAvanzaID(symbol, 5)
			if err != nil {
				return FinancialsResponse{}, fmt.Errorf("[Financials] Avanza: %w", err)
			}
			id = resolved
		}
		ticker, err := avanzaYahooTicker(id)
		if err != nil {
			return FinancialsResponse{}, fmt.Errorf("[Financials] Avanza: %w", err)
		}
		res, err := fetchYahooFinancials(ticker)
		if err != nil {
			return FinancialsResponse{}, err
		}
		res.Symbol = symbol
		res.Provider = "AVANZA"
		return res, nil
	case ProviderFinnhub:
		if finnhubKey == "" {
			return FinancialsResponse{}, fmt.Errorf("[Financials] Finnhub financials require FINNHUB_API_KEY")
		}
		return fetchFinnhubFinancials(symbol, finnhubKey)
	case ProviderBinance:
		return FinancialsResponse{
			Symbol:   symbol,
			Provider: "BINANCE",
			Source:   "none",
			Reports:  []QuarterlyFinancial{},
			Note:     "Crypto pairs have no quarterly financial reports.",
		}, nil
	default:
		return FinancialsResponse{}, fmt.Errorf("[Financials] no engine for provider %s", provider)
	}
}

func fptr(v float64) *float64 { return &v }

// roundMoney trims float error from cumulative subtraction (nearest whole
// unit); roundEPS keeps 4 decimals.
func roundMoney(p *float64) *float64 {
	if p == nil {
		return nil
	}
	return fptr(math.Round(*p))
}

func roundEPS(p *float64) *float64 {
	if p == nil {
		return nil
	}
	return fptr(math.Round(*p*1e4) / 1e4)
}

// quarterLabel turns "2026-06-30" into "Q2 2026".
func quarterLabel(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	q := (int(t.Month())-1)/3 + 1
	return fmt.Sprintf("CY%d Q%d", t.Year(), q)
}

// ---------------------------------------------------------------------------
// Yahoo Finance — fundamentals-timeseries API
// ---------------------------------------------------------------------------

func fetchYahooFinancials(symbol string) (FinancialsResponse, error) {
	formatted := strings.ToUpper(symbol)
	now := time.Now()
	params := url.Values{}
	params.Set("symbol", formatted)
	params.Set("type", strings.Join([]string{
		"quarterlyTotalRevenue",
		"quarterlyGrossProfit",
		"quarterlyOperatingIncome",
		"quarterlyNetIncome",
		"quarterlyDilutedEPS",
		"quarterlyFreeCashFlow",
	}, ","))
	params.Set("period1", fmt.Sprintf("%d", now.AddDate(-5, 0, 0).Unix()))
	params.Set("period2", fmt.Sprintf("%d", now.AddDate(0, 0, 1).Unix()))
	params.Set("merge", "false")

	endpoint := "https://query1.finance.yahoo.com/ws/fundamentals-timeseries/v1/finance/timeseries/" +
		url.PathEscape(formatted) + "?" + params.Encode()

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return FinancialsResponse{}, fmt.Errorf("[YahooFinancials] HTTP failed for %s: %w", formatted, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return FinancialsResponse{}, fmt.Errorf("[YahooFinancials] parse failed for %s: %w", formatted, err)
	}

	ts, _ := parsed["timeseries"].(map[string]any)
	results, _ := ts["result"].([]any)

	byDate := map[string]*QuarterlyFinancial{}
	for _, item := range results {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		meta, _ := entry["meta"].(map[string]any)
		types, _ := meta["type"].([]any)
		if len(types) == 0 {
			continue
		}
		typeName, _ := types[0].(string)
		values, _ := entry[typeName].([]any)

		for _, raw := range values {
			v, _ := raw.(map[string]any)
			if v == nil {
				continue
			}
			date := safeString(v, "asOfDate")
			if date == "" {
				continue
			}
			rv, _ := v["reportedValue"].(map[string]any)
			if rv == nil {
				continue
			}
			num, ok := rv["raw"].(float64)
			if !ok {
				continue
			}

			row := byDate[date]
			if row == nil {
				row = &QuarterlyFinancial{
					Period:      quarterLabel(date),
					PeriodBasis: "calendar",
					PeriodEnd:   date,
					Currency:    safeString(v, "currencyCode"),
				}
				byDate[date] = row
			}
			switch typeName {
			case "quarterlyTotalRevenue":
				row.Revenue = fptr(num)
			case "quarterlyGrossProfit":
				row.GrossProfit = fptr(num)
			case "quarterlyOperatingIncome":
				row.OperatingIncome = fptr(num)
			case "quarterlyNetIncome":
				row.NetIncome = fptr(num)
			case "quarterlyDilutedEPS":
				row.EPS = fptr(num)
			case "quarterlyFreeCashFlow":
				row.FreeCashFlow = fptr(num)
			}
		}
	}

	reports := make([]QuarterlyFinancial, 0, len(byDate))
	for _, row := range byDate {
		reports = append(reports, *row)
	}
	// Some Yahoo company fundamentals map the same EBIT metric to both
    // "quarterlyGrossProfit" and "quarterlyOperatingIncome" (e.g. CGI).
    // Do not manufacture a gross margin from an indistinguishable value.
    for i:=range reports{
      row:=&reports[i]
      if row.GrossProfit!=nil && row.OperatingIncome!=nil && *row.GrossProfit==*row.OperatingIncome && *row.GrossProfit!=0{
        row.GrossProfit=nil
        row.DataQualityWarnings=append(row.DataQualityWarnings,"gross_profit omitted: provider reported exactly the same value as operating_income; verify official statements")
      }
    }
    sortByPeriodDesc(reports)

	res := FinancialsResponse{
		Symbol:   formatted,
		Provider: "YAHOO",
		Source:   "yahoo",
		Reports:  reports,
	}
	if len(reports) == 0 {
		res.Note = "No quarterly statements published for this instrument."
	}
	return res, nil
}

func sortByPeriodDesc(reports []QuarterlyFinancial) {
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].PeriodEnd > reports[j].PeriodEnd
	})
}

// ---------------------------------------------------------------------------
// Finnhub — financials-reported (SEC filings, income statement concepts)
// ---------------------------------------------------------------------------

func fetchFinnhubFinancials(symbol, apiKey string) (FinancialsResponse, error) {
	formatted := strings.ToUpper(symbol)
	endpoint := fmt.Sprintf(
		"https://finnhub.io/api/v1/stock/financials-reported?symbol=%s&freq=quarterly&token=%s",
		url.QueryEscape(formatted), url.QueryEscape(apiKey))

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return FinancialsResponse{}, fmt.Errorf("[FinnhubFinancials] HTTP failed for %s: %w", formatted, err)
	}

	type icItem struct {
		Concept string  `json:"concept"`
		Label   string  `json:"label"`
		Value   float64 `json:"value"`
	}
	var parsed struct {
		Data []struct {
			Year      int    `json:"year"`
			Quarter   int    `json:"quarter"`
			Form      string `json:"form"`
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
			Report    struct {
				IC []icItem `json:"ic"`
			} `json:"report"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return FinancialsResponse{}, fmt.Errorf("[FinnhubFinancials] parse failed for %s: %w", formatted, err)
	}

	// SEC 10-Q income statements report year-to-date values; sort ascending and
	// subtract the previous report's YTD figure to recover per-quarter numbers.
	rows := parsed.Data
	sort.Slice(rows, func(i, j int) bool { return rows[i].EndDate < rows[j].EndDate })

	reports := make([]QuarterlyFinancial, 0, len(rows))
	var prevRev, prevGross, prevOp, prevNI, prevEPS, prevFCF *float64
	for _, r := range rows {
		periodEnd := r.EndDate
		if i := strings.IndexByte(periodEnd, ' '); i > 0 {
			periodEnd = periodEnd[:i]
		}
		cumulative := monthsBetween(r.StartDate, r.EndDate) > 4

		row := QuarterlyFinancial{
			Period:      fmt.Sprintf("Q%d FY%d", r.Quarter, r.Year),
			PeriodBasis: "fiscal",
			PeriodEnd:   periodEnd,
			Currency:    "USD",
			Form:        r.Form,
		}
		var rev, gross, op, ni, eps, fcf *float64
		for _, item := range r.Report.IC {
			concept := item.Concept
			label := strings.ToLower(item.Label)
			switch {
			case rev == nil && (strings.Contains(concept, "RevenueFromContractWithCustomer") ||
				strings.HasSuffix(concept, "Revenues") ||
				label == "total revenue" || label == "revenue" || label == "net sales"):
				rev = fptr(item.Value)
			case gross == nil && strings.Contains(concept, "GrossProfit"):
				gross = fptr(item.Value)
			case op == nil && strings.Contains(concept, "OperatingIncomeLoss"):
				op = fptr(item.Value)
			case ni == nil && strings.HasSuffix(concept, "NetIncomeLoss"):
				ni = fptr(item.Value)
			case eps == nil && strings.Contains(concept, "EarningsPerShareDiluted"):
				eps = fptr(item.Value)
			case fcf == nil && strings.Contains(concept, "FreeCashFlow"):
				fcf = fptr(item.Value)
			}
		}

		toQuarter := func(cur, prev *float64) *float64 {
			if cur == nil {
				return nil
			}
			if !cumulative || prev == nil {
				return cur
			}
			return fptr(*cur - *prev)
		}
		row.Revenue = roundMoney(toQuarter(rev, prevRev))
		row.GrossProfit = roundMoney(toQuarter(gross, prevGross))
		row.OperatingIncome = roundMoney(toQuarter(op, prevOp))
		row.NetIncome = roundMoney(toQuarter(ni, prevNI))
		row.EPS = roundEPS(toQuarter(eps, prevEPS))
		row.FreeCashFlow = roundMoney(toQuarter(fcf, prevFCF))

		// Keep the last known YTD value per concept; a missing concept in one
		// filing must not break the delta chain for later quarters.
		if rev != nil {
			prevRev = rev
		}
		if gross != nil {
			prevGross = gross
		}
		if op != nil {
			prevOp = op
		}
		if ni != nil {
			prevNI = ni
		}
		if eps != nil {
			prevEPS = eps
		}
		if fcf != nil {
			prevFCF = fcf
		}
		reports = append(reports, row)
	}
	sortByPeriodDesc(reports)
	if len(reports) > 8 {
		reports = reports[:8]
	}

	res := FinancialsResponse{
		Symbol:   formatted,
		Provider: "FINNHUB",
		Source:   "finnhub",
		Reports:  reports,
	}
	if len(reports) == 0 {
		res.Note = "No quarterly SEC filings returned by Finnhub for this instrument."
	}
	return res, nil
}

// monthsBetween returns the whole-month distance between two
// "2006-01-02 15:04:05" timestamps (0 on parse failure).
func monthsBetween(start, end string) int {
	parse := func(s string) (time.Time, bool) {
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	a, okA := parse(start)
	b, okB := parse(end)
	if !okA || !okB || b.Before(a) {
		return 0
	}
	months := (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
	return months
}
