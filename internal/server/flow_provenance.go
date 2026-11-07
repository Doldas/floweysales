package server

import (
	"encoding/json"
	"strings"
	"time"

	"floweysales/internal/provenance"
)

// Every investor node that produces data stamps each row with a "provenance"
// object (internal/provenance.Fact), so the origin travels with the row through
// later nodes, Markdown and JSON exports. Nodes that only select or format the
// rows of an earlier node keep the fact the rows already carry.

// flowNodeProvenance classifies every investor node type. A new node type must
// be added here (a test fails otherwise): "stamped" nodes create data, the
// others pass rows through or send them.
var flowNodeProvenance = map[string]string{
	"watchlist_quotes": "stamped", "technical_scan": "stamped", "fundamentals": "stamped", "sec_filings": "stamped",
	"news": "stamped", "leaderboard": "stamped", "insider_activity": "stamped", "macro_regime": "stamped",
	"portfolio_check": "stamped", "corporate_events": "stamped", "wealth_plan": "stamped", "backtest": "stamped",
	"screen": "passthrough", "only_new": "passthrough", "report_table": "passthrough", "notify": "passthrough",
}

func rowString(row map[string]any, key string) string {
	s, _ := row[key].(string)
	return s
}

func rowDate(row map[string]any, keys ...string) *time.Time {
	for _, k := range keys {
		if t, err := time.Parse("2006-01-02", rowString(row, k)); err == nil {
			return &t
		}
		if t, err := time.Parse(time.RFC3339, rowString(row, k)); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// flowRowFact is the origin of one row a node produced. provider is the node's
// resolved provider name; now is when the node ran.
func flowRowFact(n flowNode, provider string, row map[string]any, now time.Time) provenance.Fact {
	now = now.UTC()
	symbol := rowString(row, "symbol")
	if p := rowString(row, "provider"); p != "" {
		provider = p
	}
	var f provenance.Fact
	switch n.Type {
	case "watchlist_quotes":
		f = providerFact(provider, symbol, derefTime(rowDate(row, "as_of")), now, "Price as the provider returned it; FloweySales does not verify it.")
	case "fundamentals":
		f = providerFact(provider, symbol, derefTime(rowDate(row, "as_of", "ttm_through")), now, "Reported quarters and the last price as the provider returned them; ratios are calculated from them.")
	case "corporate_events":
		f = providerFact(provider, symbol, derefTime(rowDate(row, "date")), now, "A date the provider listed; dates can move.")
	case "news":
		f = providerFact(provider, symbol, time.Time{}, now, "Found by the provider's news feed; the article itself was not read.")
		if u := rowString(row, "url"); u != "" {
			f.URL, f.Status = u, provenance.Known
		}
		f.PublishedAt = rowDate(row, "published") // nil when the provider gave none: never guessed
	case "sec_filings":
		f = secProvenance(rowString(row, "url"), "", now, "A filing listed by EDGAR; "+rowString(row, "form")+" filed "+rowString(row, "filed")+".")
		f.PublishedAt = rowDate(row, "filed")
	case "insider_activity":
		f = secProvenance(rowString(row, "url"), "SEC EDGAR Form 4", now, "Insider transactions from Form 4 filings; the link is the latest filing.")
	case "technical_scan":
		f = calculatedRowFact("flow.technical_scan", "Indicators (moving averages, RSI, highs and lows) computed from one year of the provider's daily bars.", provider, symbol, rowDate(row, "as_of"), now)
	case "leaderboard":
		f = calculatedRowFact("leaderboard."+n.Strategy, "Each metric becomes a percentile within the list; a factor is the weighted average of its metrics; the score is the weighted average of the factors.", provider, symbol, rowDate(row, "as_of"), now)
		f.Method.Version = leaderboardMethodologyVersion
		f.Inputs = append(f.Inputs, "universe:"+n.Universe)
	case "macro_regime":
		f = calculatedRowFact("macro.regime", "A regime label derived from market proxies read from the provider.", provider, "", rowDate(row, "as_of"), now)
	case "portfolio_check":
		f = calculatedRowFact("portfolio.check", "Checks computed from the simulated paper account and the provider's daily closes.", provider, symbol, nil, now)
		f.Inputs = append(f.Inputs, "paper_account")
		f.Note = "The paper account is simulated, not a brokerage record."
	case "wealth_plan":
		f = calculatedRowFact("wealth_plan", "Computed from the plan you saved.", "", "", nil, now)
		f.Inputs = []string{"user_plan"}
		f.Status = provenance.Known
		f.Note = "The plan's inputs are your assumptions; results are model output, not forecasts."
	case "backtest":
		f = calculatedRowFact("backtest.flow", "Rank the symbols at each rebalance date using only data reported by then, hold the top names, subtract costs.", provider, symbol, rowDate(row, "data_end", "as_of"), now)
		f.Note = backtestNote
	default:
		return provenance.Unspecified("This node type records no source.")
	}
	if err := f.Validate(); err != nil {
		return provenance.Unspecified("provenance could not be built: " + err.Error())
	}
	return f
}

func calculatedRowFact(methodID, formula, provider, symbol string, asOf *time.Time, now time.Time) provenance.Fact {
	f := provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, Provider: provider, RetrievedAt: &now, AsOf: asOf,
		Method: &provenance.Method{ID: methodID, Version: "1", Formula: formula},
		Note:   "Computed from provider data; FloweySales does not verify the inputs.",
	}
	if provider != "" {
		f.Inputs = []string{"provider:" + provider}
		if link := providerRecordURL(provider, symbol); link != nil {
			f.URL = *link
		}
	} else {
		f.Status = provenance.Partial
	}
	return f
}

// stampFlowProvenance adds a provenance fact to every row of a stamped node's
// output that has none. Output that is not a JSON array of objects (an error
// message, text) is returned unchanged, as are rows that are errors.
func stampFlowProvenance(n flowNode, provider, output string, now time.Time) string {
	if flowNodeProvenance[n.Type] != "stamped" {
		return output
	}
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "[") {
		return output
	}
	var rows []map[string]any
	if json.Unmarshal([]byte(trimmed), &rows) != nil {
		return output
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		if _, failed := row["error"]; failed {
			continue
		}
		if _, has := row["provenance"]; has {
			continue
		}
		row["provenance"] = flowRowFact(n, provider, row, now)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return output
	}
	return string(out)
}
