// Command universe-coverage is the spike behind issue #209: it checks whether
// candidate markets (Tokyo, Hong Kong, India, Korea, Johannesburg and others)
// have the prices and reports the leaderboard needs, so a maintainer can decide
// which markets to offer. It enables nothing by itself.
//
// Three modes, from least to most access:
//
//	universe-coverage                          offline: validate the candidate lists
//	universe-coverage -input saved/            offline: judge saved leaderboard responses
//	universe-coverage -url http://localhost:8080 -save saved/
//	                                            live: fetch each market from a running
//	                                            server whose provider can reach the markets
//
// Saved responses are one file per market, named <market>.json, as returned by
// GET /api/market/leaderboard?symbols=...&strategy=best. A live run needs a
// signed-in session when authentication is on: pass its cookie with -cookie
// (the value of the flowey_session cookie).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"floweysales/internal/universecheck"
)

func main() {
	var (
		input    = flag.String("input", "", "directory of saved leaderboard responses, <market>.json each")
		base     = flag.String("url", "", "base URL of a running FloweySales server to fetch from")
		cookie   = flag.String("cookie", os.Getenv("FLOWEY_SESSION"), "value of the flowey_session cookie (default $FLOWEY_SESSION)")
		provider = flag.String("provider", "", "market-data provider id, empty for the server's default")
		save     = flag.String("save", "", "with -url, also save each raw response to this directory")
		only     = flag.String("markets", "", "comma-separated market ids to check (default all)")
		out      = flag.String("out", "", "write the JSON report to this file")
		strict   = flag.Bool("strict", false, "exit 1 unless every market with candidates reaches a full verdict")
	)
	flag.Parse()

	markets := universecheck.Candidates()
	if *only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(*only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		kept := markets[:0]
		for _, m := range markets {
			if want[m.ID] {
				delete(want, m.ID)
				kept = append(kept, m)
			}
		}
		for id := range want {
			fatal(fmt.Errorf("unknown market %q", id))
		}
		markets = kept
	}

	bad := false
	for _, m := range markets {
		for _, p := range universecheck.Validate(m) {
			fmt.Printf("INVALID %-13s %s\n", m.ID, p)
			bad = true
		}
	}
	if bad {
		os.Exit(2)
	}
	if *input == "" && *base == "" {
		fmt.Println("Offline check passed: the candidate definitions are well formed. Nothing was measured.")
		for _, m := range markets {
			if len(m.Symbols) == 0 {
				fmt.Printf("  %-13s no candidate symbols (%s)\n", m.ID, m.Note)
				continue
			}
			fmt.Printf("  %-13s %d candidates, expects %s\n", m.ID, len(m.Symbols), strings.Join(m.Quoted, "/"))
		}
		fmt.Println("Measure with -url (needs provider access) or -input (saved responses). See docs/universe-coverage.md.")
		return
	}

	now := time.Now().UTC()
	var results []universecheck.Result
	for _, m := range markets {
		var resp *universecheck.Response
		if len(m.Symbols) > 0 {
			raw, err := load(*input, *base, *cookie, *provider, m)
			switch {
			case err != nil && *base != "":
				fmt.Printf("ERROR   %-13s %v\n", m.ID, err)
			case err != nil:
				fmt.Printf("MISSING %-13s %v\n", m.ID, err)
			default:
				if *save != "" && *base != "" {
					if err := os.MkdirAll(*save, 0o755); err == nil {
						err = os.WriteFile(filepath.Join(*save, m.ID+".json"), raw, 0o644)
					}
					if err != nil {
						fatal(err)
					}
				}
				r, err := universecheck.ParseResponse(raw)
				if err != nil {
					fmt.Printf("ERROR   %-13s %v\n", m.ID, err)
				} else {
					resp = &r
				}
			}
		}
		results = append(results, universecheck.Evaluate(m, resp, now))
	}

	fmt.Printf("%-13s %-12s %7s %7s %9s  %s\n", "market", "verdict", "priced", "1y", "4 quarters", "currency / newest price")
	allFull := true
	for _, r := range results {
		fmt.Printf("%-13s %-12s %6.0f%% %6.0f%% %9.0f%%  %s / %s\n", r.Market, r.Verdict, r.PricedPct, r.YearPct, r.ReportsPct, strings.Join(r.Currencies, ","), r.NewestPrice)
		for _, f := range r.Findings {
			fmt.Printf("    - %s\n", f)
		}
		if r.Verdict != universecheck.Full && r.Verdict != universecheck.NoSymbols {
			allFull = false
		}
	}
	fmt.Println("\nA full verdict does not enable a market: add a reviewed list to internal/server/universes.json and update docs.")
	if *out != "" {
		body, _ := json.MarshalIndent(map[string]any{"generated_at": now.Format(time.RFC3339), "bar_share": universecheck.BarShare, "results": results}, "", "  ")
		if err := os.WriteFile(*out, append(body, '\n'), 0o644); err != nil {
			fatal(err)
		}
	}
	if *strict && !allFull {
		os.Exit(1)
	}
}

// load returns the raw leaderboard response for a market, from a saved file or
// from a running server.
func load(dir, base, cookie, provider string, m universecheck.Market) ([]byte, error) {
	if dir != "" {
		return os.ReadFile(filepath.Join(dir, m.ID+".json"))
	}
	q := url.Values{"symbols": {strings.Join(m.Symbols, ",")}, "strategy": {"best"}, "preferences": {"off"}}
	if provider != "" {
		q.Set("provider", provider)
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/api/market/leaderboard?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "flowey_session", Value: cookie})
	}
	// A cold market takes a while: the server fetches a year of prices and reports per symbol.
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	return body, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "universe-coverage:", err)
	os.Exit(2)
}
