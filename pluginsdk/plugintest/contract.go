package plugintest

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

// Behavior is what FloweySales relies on from every provider of a capability,
// whichever service stands behind it. Conformance checks that a plugin is
// wired correctly; Contract checks what it answers: the same assertions run
// against every provider, so a second provider of a domain is held to what the
// first one is. It needs no network: the plugin is built against a local
// upstream the test controls.
type Behavior struct {
	// New builds the plugin with its upstream at base, the URL of a local test server.
	New func(base string) sdk.Plugin
	// Healthy answers like the real upstream does for a good request, for every
	// capability the contract exercises.
	Healthy http.Handler
	// Symbol is an instrument Healthy knows.
	Symbol string
	// Range is the history range to ask for; sdk.Range1M when empty.
	Range string
	// From and To are the events window; two weeks of 2026 when empty.
	From, To string
	// Skip names a capability the contract does not exercise for this plugin,
	// with the reason. A capability that is neither declared nor skipped is exercised.
	Skip map[string]string
}

// contractCapabilities are the capabilities Contract exercises, in order.
var contractCapabilities = []string{sdk.CapQuote, sdk.CapHistory, sdk.CapNews, sdk.CapEvents, sdk.CapProfile}

// Contract runs the behavior checks, one subtest per capability the plugin declares:
//   - a healthy upstream gives a result that passes the Check function of its type;
//   - an upstream that fails (HTTP 500) is reported as an error, never as an empty result;
//   - an upstream that answers with something that is not what it promised is reported
//     as an error too, so garbage is never shown as data;
//   - an empty symbol is "invalid_argument" and does not reach the upstream.
func Contract(t *testing.T, b Behavior) {
	t.Helper()
	for _, capability := range contractCapabilities {
		t.Run(capability, func(t *testing.T) { contract(t, b, capability) })
	}
}

// failingUpstream answers every request with a server error.
var failingUpstream = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "upstream down", http.StatusInternalServerError)
})

// garbageUpstream answers 200 with a page that is no data of any kind.
var garbageUpstream = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte("<html><body>Service temporarily unavailable, please try again</body></html>"))
})

func contract(t TB, b Behavior, capability string) {
	t.Helper()
	if reason, skipped := b.Skip[capability]; skipped {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s: a skipped capability needs a reason", capability)
		}
		return
	}
	probe := b.New("http://127.0.0.1:1")
	if !probe.Manifest.Provider.HasCapability(capability) {
		return
	}
	call, ok := callFor(b, capability)
	if !ok {
		t.Fatalf("%s: no contract for this capability", capability)
	}

	run := func(handler http.Handler, args string) (code string, result any, hits int32) {
		var count int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&count, 1)
			handler.ServeHTTP(w, r)
		}))
		defer srv.Close()
		c := Start(t, b.New(srv.URL))
		result, err := call(c, args)
		return errorCode(err), result, atomic.LoadInt32(&count)
	}

	code, result, _ := run(b.Healthy, b.Symbol)
	switch {
	case code != "success":
		t.Errorf("%s: a healthy upstream gave %q, want a result", capability, code)
	default:
		if err := checkResult(result); err != nil {
			t.Errorf("%s: the result breaks the contract: %v", capability, err)
		}
	}
	for name, h := range map[string]http.Handler{"an upstream that fails (HTTP 500)": failingUpstream, "an upstream that answers with garbage": garbageUpstream} {
		if code, _, _ := run(h, b.Symbol); code != sdk.CodeUpstream {
			t.Errorf("%s: %s gave %q, want %q", capability, name, code, sdk.CodeUpstream)
		}
	}
	if code, _, hits := run(b.Healthy, ""); code != sdk.CodeInvalidArgument || hits != 0 {
		t.Errorf("%s: an empty symbol gave %q after %d upstream requests, want %q and none", capability, code, hits, sdk.CodeInvalidArgument)
	}
}

func callFor(b Behavior, capability string) (func(c *Client, symbol string) (any, error), bool) {
	rng := b.Range
	if rng == "" {
		rng = sdk.Range1M
	}
	from, to := b.From, b.To
	if from == "" {
		from, to = "2026-01-01", "2026-01-15"
	}
	switch capability {
	case sdk.CapQuote:
		return func(c *Client, s string) (any, error) {
			var out sdk.Asset
			err := c.Call(sdk.MethodQuote, sdk.QuoteParams{Symbol: s}, &out)
			return out, err
		}, true
	case sdk.CapHistory:
		return func(c *Client, s string) (any, error) {
			var out sdk.HistorySeries
			err := c.Call(sdk.MethodHistory, sdk.HistoryParams{Symbol: s, Range: rng}, &out)
			return out, err
		}, true
	case sdk.CapNews:
		return func(c *Client, s string) (any, error) {
			var out []sdk.NewsItem
			err := c.Call(sdk.MethodNews, sdk.NewsParams{Symbol: s, Limit: 5}, &out)
			return out, err
		}, true
	case sdk.CapEvents:
		return func(c *Client, s string) (any, error) {
			var out []sdk.CorporateEvent
			err := c.Call(sdk.MethodEvents, sdk.EventsParams{Symbol: s, From: from, To: to}, &out)
			return out, err
		}, true
	case sdk.CapProfile:
		return func(c *Client, s string) (any, error) {
			var out sdk.InstrumentProfile
			err := c.Call(sdk.MethodProfile, sdk.ProfileParams{Symbol: s}, &out)
			return out, err
		}, true
	}
	return nil, false
}

func checkResult(result any) error {
	switch v := result.(type) {
	case sdk.Asset:
		return CheckAsset(v)
	case sdk.HistorySeries:
		return CheckHistory(v)
	case []sdk.NewsItem:
		return CheckNews(v)
	case []sdk.CorporateEvent:
		return CheckEvents(v)
	case sdk.InstrumentProfile:
		return CheckProfile(v)
	}
	return fmt.Errorf("unexpected result type %T", result)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// CheckAsset reports what is wrong with a quote: no symbol, source or currency,
// or a price that is not a positive number.
func CheckAsset(a sdk.Asset) error {
	var errs []error
	if a.Symbol == "" {
		errs = append(errs, errors.New("quote has no symbol"))
	}
	if a.Provider == "" {
		errs = append(errs, errors.New("quote does not name the provider that served it"))
	}
	if a.Currency == "" {
		errs = append(errs, errors.New("quote has no currency"))
	}
	if !finite(a.Price) || a.Price <= 0 {
		errs = append(errs, fmt.Errorf("quote price %v is not a positive number", a.Price))
	}
	return errors.Join(errs...)
}

// CheckHistory reports what is wrong with a price series: it is empty, not in
// time order, in milliseconds instead of seconds, or has bars that cannot exist.
func CheckHistory(s sdk.HistorySeries) error {
	var errs []error
	if s.Symbol == "" || s.Provider == "" {
		errs = append(errs, errors.New("series names no symbol or provider"))
	}
	if len(s.Points) == 0 {
		return errors.Join(append(errs, errors.New("series has no bars"))...)
	}
	// 2000-01-01 and 2100-01-01 in Unix seconds: a millisecond timestamp is far beyond the second.
	const minSeconds, maxSeconds = 946684800, 4102444800
	var previous int64
	for i, p := range s.Points {
		switch {
		case p.Timestamp < minSeconds || p.Timestamp > maxSeconds:
			errs = append(errs, fmt.Errorf("bar %d time %d is not Unix seconds", i, p.Timestamp))
		case i > 0 && p.Timestamp <= previous:
			errs = append(errs, fmt.Errorf("bar %d is not after the bar before it", i))
		}
		previous = p.Timestamp
		for _, v := range []float64{p.Open, p.High, p.Low, p.Close} {
			if !finite(v) || v <= 0 {
				errs = append(errs, fmt.Errorf("bar %d has a price %v that is not a positive number", i, v))
				break
			}
		}
		if p.High < math.Max(p.Open, p.Close) || p.Low > math.Min(p.Open, p.Close) || p.High < p.Low {
			errs = append(errs, fmt.Errorf("bar %d: open %v high %v low %v close %v do not fit one bar", i, p.Open, p.High, p.Low, p.Close))
		}
		if !finite(p.Volume) || p.Volume < 0 {
			errs = append(errs, fmt.Errorf("bar %d has volume %v", i, p.Volume))
		}
		if len(errs) > 5 {
			break
		}
	}
	return errors.Join(errs...)
}

// CheckNews reports what is wrong with headlines: a missing title, a link that
// is not a public https address, or a date that is not Unix seconds.
func CheckNews(items []sdk.NewsItem) error {
	var errs []error
	for i, n := range items {
		if strings.TrimSpace(n.Title) == "" {
			errs = append(errs, fmt.Errorf("headline %d has no title", i))
		}
		if !strings.HasPrefix(n.URL, "https://") {
			errs = append(errs, fmt.Errorf("headline %d link %q is not an https address", i, n.URL))
		}
		if n.Published != 0 && (n.Published < 946684800 || n.Published > 4102444800) {
			errs = append(errs, fmt.Errorf("headline %d time %d is not Unix seconds", i, n.Published))
		}
	}
	return errors.Join(errs...)
}

var (
	isoDate  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	splitRat = regexp.MustCompile(`^\d+(\.\d+)?:\d+(\.\d+)?$`)
)

// CheckEvents reports what is wrong with corporate events: an unknown kind, a
// date that is not YYYY-MM-DD, a dividend without a positive amount or a split
// whose ratio is not "4:1".
func CheckEvents(events []sdk.CorporateEvent) error {
	var errs []error
	for i, e := range events {
		if e.Symbol == "" {
			errs = append(errs, fmt.Errorf("event %d has no symbol", i))
		}
		if _, err := time.Parse("2006-01-02", e.Date); err != nil || !isoDate.MatchString(e.Date) {
			errs = append(errs, fmt.Errorf("event %d date %q is not YYYY-MM-DD", i, e.Date))
		}
		switch e.Kind {
		case "dividend":
			if e.Amount != nil && (!finite(*e.Amount) || *e.Amount <= 0) {
				errs = append(errs, fmt.Errorf("event %d dividend amount %v is not positive", i, *e.Amount))
			}
		case "split":
			if !splitRat.MatchString(e.Ratio) {
				errs = append(errs, fmt.Errorf("event %d split ratio %q is not like 4:1", i, e.Ratio))
			}
		case "earnings":
		default:
			errs = append(errs, fmt.Errorf("event %d has kind %q, want dividend, split or earnings", i, e.Kind))
		}
	}
	return errors.Join(errs...)
}

var (
	isoCountry  = regexp.MustCompile(`^[A-Z]{2}$`)
	isoCurrency = regexp.MustCompile(`^[A-Z]{3,5}$`) // ISO 4217, and the longer codes of crypto
	isoMIC      = regexp.MustCompile(`^[A-Z0-9]{4}$`)
)

// CheckProfile reports what is wrong with an instrument profile: a kind that
// does not exist, codes that are not ISO shaped, a description over the limit,
// a time that is not RFC 3339 UTC, or details of a kind the profile is not.
func CheckProfile(p sdk.InstrumentProfile) error {
	var errs []error
	if p.Symbol == "" {
		errs = append(errs, errors.New("profile has no symbol"))
	}
	switch p.Kind {
	case sdk.ProfileCompany, sdk.ProfileFund, sdk.ProfileCommodity, sdk.ProfileCrypto, sdk.ProfileIndex, sdk.ProfileCurrency, sdk.ProfileOther:
	default:
		errs = append(errs, fmt.Errorf("profile kind %q does not exist", p.Kind))
	}
	for _, c := range []struct {
		name, value string
		pattern     *regexp.Regexp
	}{{"country", p.Country, isoCountry}, {"currency", p.Currency, isoCurrency}, {"MIC", p.MIC, isoMIC}} {
		if c.value != "" && !c.pattern.MatchString(c.value) {
			errs = append(errs, fmt.Errorf("profile %s %q is not an ISO code", c.name, c.value))
		}
	}
	if len([]rune(p.Description)) > 4000 {
		errs = append(errs, errors.New("profile description is over 4000 characters"))
	}
	if p.AsOf != "" {
		if at, err := time.Parse(time.RFC3339, p.AsOf); err != nil || at.UTC().Format(time.RFC3339) != p.AsOf {
			errs = append(errs, fmt.Errorf("profile as_of %q is not RFC 3339 in UTC", p.AsOf))
		}
	}
	for kind, present := range map[string]bool{sdk.ProfileCompany: p.Company != nil, sdk.ProfileFund: p.Fund != nil, sdk.ProfileCommodity: p.Commodity != nil, sdk.ProfileCrypto: p.Crypto != nil} {
		if present && p.Kind != kind {
			errs = append(errs, fmt.Errorf("a %s profile carries %s details", p.Kind, kind))
		}
	}
	return errors.Join(errs...)
}
