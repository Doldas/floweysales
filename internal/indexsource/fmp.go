package indexsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Financial Modeling Prep adapter. The user supplies the API key; the key is
// sent only to FMP, is never logged and never appears in an error returned
// from here.
//
// Verified from FMP's documentation (paths): GET /stable/sp500-constituent and
// GET /stable/historical-sp500-constituent. For Nasdaq-100 the documented
// paths are /stable/nasdaq-constituent and /stable/historical-nasdaq-constituent.
//
// NOT verified: the response field names. FMP's documentation pages could not
// be read from the build environment (403), so the parser accepts the names
// commonly seen (symbol, name; for changes symbol, removedTicker, date or
// dateAdded) and refuses a response in which it finds none of them, rather
// than guessing. The fixtures under testdata are hand-written to that shape.
// Nasdaq-100 carries the Unverified flag until someone confirms it against
// a live response.

// DefaultFMPBase is FMP's API host.
const DefaultFMPBase = "https://financialmodelingprep.com"

// HTTPDoer is the part of *http.Client the adapter needs; tests inject a fake.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// FMP reads constituent lists with the user's key.
type FMP struct {
	Client HTTPDoer
	Base   string
	Key    string
}

// FMPIndex says which FMP endpoints serve an index.
type FMPIndex struct {
	Current, History string
	Unverified       bool
}

// FMPIndexes covers the indices FMP can serve. OMXS30 is absent on purpose:
// no FMP endpoint for it is known, so it is CSV only.
var FMPIndexes = map[string]FMPIndex{
	"index_sp500":     {Current: "sp500-constituent", History: "historical-sp500-constituent"},
	"index_nasdaq100": {Current: "nasdaq-constituent", History: "historical-nasdaq-constituent", Unverified: true},
}

const fmpMaxBody = 4 << 20

// ErrFMPAuth means FMP rejected the key or the plan does not include the data.
var ErrFMPAuth = errors.New("Financial Modeling Prep refused the request: the key is invalid or your plan does not include this data")

func (f FMP) get(ctx context.Context, path string, out any) error {
	base := f.Base
	if base == "" {
		base = DefaultFMPBase
	}
	u := strings.TrimRight(base, "/") + "/stable/" + path + "?apikey=" + url.QueryEscape(f.Key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return errors.New("cannot build the request")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.Client.Do(req)
	if err != nil {
		// A transport error carries the URL, and the URL carries the key.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("cannot reach Financial Modeling Prep: %s", f.scrub(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, fmpMaxBody+1))
	if err != nil {
		return errors.New("cannot read the Financial Modeling Prep response")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusPaymentRequired:
		return ErrFMPAuth
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("Financial Modeling Prep rate limit reached; try again later")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("Financial Modeling Prep answered HTTP %d", resp.StatusCode)
	case len(body) > fmpMaxBody:
		return errors.New("the Financial Modeling Prep response is too large")
	}
	if err := json.Unmarshal(body, out); err != nil {
		// FMP reports plan and key errors as a JSON object with HTTP 200.
		var msg map[string]any
		if json.Unmarshal(body, &msg) == nil {
			return ErrFMPAuth
		}
		return errors.New("the Financial Modeling Prep response is not the expected JSON list")
	}
	return nil
}

func (f FMP) scrub(s string) string {
	if f.Key != "" {
		s = strings.ReplaceAll(s, f.Key, "***")
	}
	return s
}

type fmpCurrent struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

type fmpChange struct {
	Symbol        string `json:"symbol"`
	RemovedTicker string `json:"removedTicker"`
	Date          string `json:"date"`
	DateAdded     string `json:"dateAdded"`
}

// Fetch builds a snapshot for the index from FMP: today's members, and the
// dated add/remove changes for point-in-time membership.
func (f FMP) Fetch(ctx context.Context, spec Spec, meta Meta, now time.Time) (Snapshot, []string, error) {
	ep, ok := FMPIndexes[spec.ID]
	if !ok {
		return Snapshot{}, []string{spec.Name + " has no Financial Modeling Prep source here; import a CSV file instead"}, errors.New("unsupported")
	}
	if err := Confirm(meta.LicenseTerms, meta.LicenseConfirm); err != nil {
		return Snapshot{}, []string{err.Error()}, err
	}
	if strings.TrimSpace(f.Key) == "" {
		return Snapshot{}, []string{"no Financial Modeling Prep key is saved"}, errors.New("no key")
	}
	var cur []fmpCurrent
	if err := f.get(ctx, ep.Current, &cur); err != nil {
		return Snapshot{}, []string{err.Error()}, err
	}
	snap := Snapshot{IndexID: spec.ID, Provider: ProviderFMP, SourceName: "Financial Modeling Prep (" + ep.Current + ")",
		AsOf: now.UTC().Format(DateLayout), ImportedAt: now.UTC().Format(time.RFC3339), LicenseTerms: strings.TrimSpace(meta.LicenseTerms),
		LicenseConfirm: true, Warnings: []string{}}
	seen := map[string]bool{}
	var problems []string
	for _, c := range cur {
		sym, ok := NormalizeUS(c.Symbol)
		if !ok {
			if strings.TrimSpace(c.Symbol) != "" {
				problems = append(problems, fmt.Sprintf("unusable symbol %q in the response", c.Symbol))
			}
			continue
		}
		if seen[sym] {
			problems = append(problems, fmt.Sprintf("%s appears twice in the response", sym))
			continue
		}
		seen[sym] = true
		snap.Members = append(snap.Members, Member{Symbol: sym, Name: strings.TrimSpace(c.Name)})
	}
	if len(cur) > 0 && len(snap.Members) == 0 {
		return Snapshot{}, []string{"the response has no field named symbol; its shape is not the one this version reads"}, errors.New("unexpected shape")
	}
	problems = append(problems, CheckCount(len(snap.Members), spec, &snap)...)
	if len(problems) > 0 {
		return Snapshot{}, problems, errors.New("the list was refused")
	}

	var hist []fmpChange
	if err := f.get(ctx, ep.History, &hist); err != nil {
		// Today's list is still useful; say that history is missing.
		snap.Warnings = append(snap.Warnings, "Past membership could not be fetched ("+err.Error()+"); the list holds today's members only, so a backtest on it is survivorship-biased.")
	} else {
		snap.Changes, snap.Warnings = parseChanges(hist, snap.Warnings, snap.AsOf)
		if len(snap.Changes) > 0 {
			snap.PointInTime = true
			snap.HistoryFrom = snap.Changes[0].Date
			for _, c := range snap.Changes {
				if c.Date < snap.HistoryFrom {
					snap.HistoryFrom = c.Date
				}
			}
			snap.Warnings = append(snap.Warnings, "Past membership is rebuilt from FMP's add/remove records back to "+snap.HistoryFrom+". Tickers can be reused by other companies and removed companies may have no price history from your market-data provider; those are skipped and named in the backtest.")
		} else {
			snap.Warnings = append(snap.Warnings, "FMP returned no usable add/remove records; the list holds today's members only, so a backtest on it is survivorship-biased.")
		}
	}
	if ep.Unverified {
		snap.Unverified = true
		snap.UnverifiedNote = "The Financial Modeling Prep response for this index has not been checked against a live response; compare the count and a few members with the index provider's own list before relying on it."
		snap.Warnings = append(snap.Warnings, snap.UnverifiedNote)
	}
	return snap, nil, nil
}

func parseChanges(hist []fmpChange, warnings []string, asOf string) ([]Change, []string) {
	var out []Change
	skipped := 0
	for _, h := range hist {
		date := normalizeDate(h.Date)
		if date == "" {
			date = normalizeDate(h.DateAdded)
		}
		added, aok := NormalizeUS(h.Symbol)
		if !aok {
			added = ""
		}
		removed, rok := NormalizeUS(h.RemovedTicker)
		if !rok {
			removed = ""
		}
		if date == "" || date > asOf || (added == "" && removed == "") {
			skipped++
			continue
		}
		out = append(out, Change{Date: date, Added: added, Removed: removed})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Date < out[b].Date })
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d add/remove records had no usable date or symbol and were left out.", skipped))
	}
	return out, warnings
}

// normalizeDate accepts 2024-11-26 and "November 26, 2024"; anything else is "".
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{DateLayout, "January 2, 2006", "Jan 2, 2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(DateLayout)
		}
	}
	return ""
}
