package plugintest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

// flaws are the ways the demo provider below can be made to misbehave.
type flaws struct {
	swallowErrors  bool // a failing or garbage upstream gives an empty result, not an error
	millisTime     bool // bars in milliseconds
	callsUpstream  bool // asks the upstream before looking at the symbol
	httpLinks      bool // headlines link to http:// pages
	noProvider     bool // quotes do not say who served them
	badProfileCode bool // a country that is not an ISO code
}

// demoPlugin is a provider of every contract capability over a tiny upstream
// that serves the SDK's own JSON shapes: GET <base>/<capability>?s=<symbol>.
func demoPlugin(t testing.TB, base string, f flaws) sdk.Plugin {
	m := fixture0(t)
	fetch := func(ctx context.Context, path, symbol string, out any) error {
		if symbol == "" && !f.callsUpstream {
			return sdk.Errorf(sdk.CodeInvalidArgument, "symbol is required")
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?s="+symbol, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("upstream answered %d", resp.StatusCode)
			} else {
				var body []byte
				if body, err = io.ReadAll(resp.Body); err == nil {
					err = json.Unmarshal(body, out)
				}
			}
		}
		if err != nil && f.swallowErrors {
			return nil
		}
		if err == nil && symbol == "" {
			return sdk.Errorf(sdk.CodeInvalidArgument, "symbol is required")
		}
		return err
	}
	return sdk.Plugin{
		Manifest: m,
		Quote: func(ctx context.Context, p sdk.QuoteParams) (a sdk.Asset, err error) {
			err = fetch(ctx, "/quote", p.Symbol, &a)
			if f.noProvider {
				a.Provider = ""
			}
			return
		},
		History: func(ctx context.Context, p sdk.HistoryParams) (s sdk.HistorySeries, err error) {
			err = fetch(ctx, "/history", p.Symbol, &s)
			if f.millisTime {
				for i := range s.Points {
					s.Points[i].Timestamp *= 1000
				}
			}
			return
		},
		News: func(ctx context.Context, p sdk.NewsParams) (items []sdk.NewsItem, err error) {
			err = fetch(ctx, "/news", p.Symbol, &items)
			if f.httpLinks {
				for i := range items {
					items[i].URL = strings.Replace(items[i].URL, "https://", "http://", 1)
				}
			}
			return
		},
		Events: func(ctx context.Context, p sdk.EventsParams) (ev []sdk.CorporateEvent, err error) {
			err = fetch(ctx, "/events", p.Symbol, &ev)
			return
		},
		Profile: func(ctx context.Context, p sdk.ProfileParams) (pr sdk.InstrumentProfile, err error) {
			err = fetch(ctx, "/profile", p.Symbol, &pr)
			if f.badProfileCode {
				pr.Country = "Sweden"
			}
			return
		},
	}
}

func fixture0(t testing.TB) sdk.Manifest {
	m := sdk.Manifest{Protocol: sdk.ProtocolVersion, ID: "demo", Name: "Demo", Version: "1.0.0"}
	m.Provider = sdk.ProviderInfo{ID: "DEMO", Label: "Demo", OfficialAPI: true, QuoteKind: "ohlc_bar_close", MinPollSeconds: 5, CacheTTLSeconds: 60,
		Capabilities: []string{sdk.CapQuote, sdk.CapHistory, sdk.CapNews, sdk.CapEvents, sdk.CapProfile}}
	if err := m.Validate(); err != nil {
		t.Fatalf("demo manifest: %v", err)
	}
	return m
}

var demoUpstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	var body any
	switch r.URL.Path {
	case "/quote":
		body = sdk.Asset{Symbol: "TEST", Name: "Test", Price: 12.5, Currency: "USD", Provider: "DEMO"}
	case "/history":
		body = sdk.HistorySeries{Symbol: "TEST", Provider: "DEMO", Range: "1mo", Interval: "1d", Currency: "USD", Points: []sdk.HistoryPoint{
			{Timestamp: 1767225600, Open: 10, High: 12, Low: 9, Close: 11, Volume: 100},
			{Timestamp: 1767312000, Open: 11, High: 13, Low: 10, Close: 12, Volume: 120},
		}}
	case "/news":
		body = []sdk.NewsItem{{Title: "Test raises outlook", URL: "https://example.com/a", Source: "Wire", Published: 1767225600, Symbol: "TEST"}}
	case "/events":
		amount := 0.25
		body = []sdk.CorporateEvent{{Symbol: "TEST", Kind: "dividend", Date: "2026-01-10", Amount: &amount, Currency: "USD", Confirmed: true}, {Symbol: "TEST", Kind: "split", Date: "2026-01-12", Ratio: "4:1", Confirmed: true}}
	case "/profile":
		body = sdk.InstrumentProfile{Symbol: "TEST", Kind: sdk.ProfileCompany, Name: "Test Inc", Country: "SE", Currency: "SEK", AsOf: "2026-01-02T03:04:05Z", Company: &sdk.CompanyProfile{}}
	default:
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(body)
})

func behavior(t testing.TB, f flaws) Behavior {
	return Behavior{New: func(base string) sdk.Plugin { return demoPlugin(t, base, f) }, Healthy: demoUpstream, Symbol: "TEST"}
}

func TestContractPassesAProviderThatKeepsIt(t *testing.T) {
	Contract(t, behavior(t, flaws{}))
}

// Each way of misbehaving is caught by the check that is meant to catch it.
func TestContractCatchesProvidersThatBreakIt(t *testing.T) {
	for name, tc := range map[string]struct {
		f    flaws
		want string
	}{
		"errors swallowed into empty results": {flaws{swallowErrors: true}, `gave "success", want "upstream"`},
		"milliseconds instead of seconds":     {flaws{millisTime: true}, "is not Unix seconds"},
		"the upstream asked for no symbol":    {flaws{callsUpstream: true}, "upstream requests"},
		"http links":                          {flaws{httpLinks: true}, "is not an https address"},
		"no provider on quotes":               {flaws{noProvider: true}, "does not name the provider"},
		"a country that is no code":           {flaws{badProfileCode: true}, "is not an ISO code"},
	} {
		t.Run(name, func(t *testing.T) {
			var failures []string
			ft := &fakeT{T: t, fail: func(msg string) { failures = append(failures, msg) }}
			for _, capability := range contractCapabilities {
				contract(ft, behavior(t, tc.f), capability)
			}
			if !strings.Contains(strings.Join(failures, "\n"), tc.want) {
				t.Fatalf("want a failure containing %q, got %q", tc.want, failures)
			}
		})
	}
}

func TestContractSkipNeedsAReason(t *testing.T) {
	b := behavior(t, flaws{})
	b.Skip = map[string]string{sdk.CapNews: " "}
	var failures []string
	contract(&fakeT{T: t, fail: func(msg string) { failures = append(failures, msg) }}, b, sdk.CapNews)
	if len(failures) != 1 || !strings.Contains(failures[0], "needs a reason") {
		t.Fatalf("failures: %q", failures)
	}
	b.Skip[sdk.CapNews] = "the demo upstream has no news for this symbol"
	failures = nil
	contract(&fakeT{T: t, fail: func(msg string) { failures = append(failures, msg) }}, b, sdk.CapNews)
	if len(failures) != 0 {
		t.Fatalf("a skip with a reason failed: %q", failures)
	}
}

func TestChecksRejectWhatTheContractForbids(t *testing.T) {
	neg := -1.0
	for name, err := range map[string]error{
		"quote price zero":        CheckAsset(sdk.Asset{Symbol: "X", Provider: "P", Currency: "USD"}),
		"history out of order":    CheckHistory(sdk.HistorySeries{Symbol: "X", Provider: "P", Points: []sdk.HistoryPoint{{Timestamp: 1767312000, Open: 1, High: 1, Low: 1, Close: 1}, {Timestamp: 1767225600, Open: 1, High: 1, Low: 1, Close: 1}}}),
		"history high below low":  CheckHistory(sdk.HistorySeries{Symbol: "X", Provider: "P", Points: []sdk.HistoryPoint{{Timestamp: 1767225600, Open: 2, High: 1, Low: 2, Close: 2}}}),
		"history empty":           CheckHistory(sdk.HistorySeries{Symbol: "X", Provider: "P"}),
		"news without title":      CheckNews([]sdk.NewsItem{{URL: "https://example.com"}}),
		"event bad date":          CheckEvents([]sdk.CorporateEvent{{Symbol: "X", Kind: "earnings", Date: "11/14/2026"}}),
		"event negative dividend": CheckEvents([]sdk.CorporateEvent{{Symbol: "X", Kind: "dividend", Date: "2026-01-01", Amount: &neg}}),
		"event bad split":         CheckEvents([]sdk.CorporateEvent{{Symbol: "X", Kind: "split", Date: "2026-01-01", Ratio: "four"}}),
		"event unknown kind":      CheckEvents([]sdk.CorporateEvent{{Symbol: "X", Kind: "merger", Date: "2026-01-01"}}),
		"profile unknown kind":    CheckProfile(sdk.InstrumentProfile{Symbol: "X", Kind: "stock"}),
		"profile local as_of":     CheckProfile(sdk.InstrumentProfile{Symbol: "X", Kind: sdk.ProfileCompany, AsOf: "2026-01-02T03:04:05+02:00"}),
		"profile wrong details":   CheckProfile(sdk.InstrumentProfile{Symbol: "X", Kind: sdk.ProfileCrypto, Company: &sdk.CompanyProfile{}}),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := CheckEvents([]sdk.CorporateEvent{{Symbol: "X", Kind: "earnings", Date: "2026-11-14"}}); err != nil {
		t.Errorf("a valid earnings event: %v", err)
	}
}
