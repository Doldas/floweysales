package pluginsdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// roundTrip decodes a contract fixture into T and checks that re-encoding
// yields the same JSON, so no field is silently dropped or renamed.
func roundTrip[T any](t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	again, _ := json.Marshal(v)
	var want, got any
	_ = json.Unmarshal(raw, &want)
	_ = json.Unmarshal(again, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%s does not round-trip:\nwant %s\ngot  %s", name, raw, again)
	}
}

func TestContractFixturesRoundTrip(t *testing.T) {
	roundTrip[HistorySeries](t, "history.json")
	roundTrip[Asset](t, "asset.json")
	roundTrip[FinancialsResponse](t, "financials.json")
	roundTrip[[]SearchResult](t, "search.json")
	roundTrip[ResearchResult](t, "research.json")
	roundTrip[Manifest](t, "manifest.json")
	roundTrip[[]NewsItem](t, "news.json")
}

func TestManifestValidation(t *testing.T) {
	raw, _ := os.ReadFile(filepath.Join("testdata", "contract", "manifest.json"))
	if _, err := ParseManifest(raw); err != nil {
		t.Fatalf("fixture manifest rejected: %v", err)
	}
	base, _ := ParseManifest(raw)
	cases := map[string]func(*Manifest){
		"protocol":          func(m *Manifest) { m.Protocol = 2 },
		"id":                func(m *Manifest) { m.ID = "../evil" },
		"version":           func(m *Manifest) { m.Version = "1.0" },
		"provider id":       func(m *Manifest) { m.Provider.ID = "yahoo" },
		"capability":        func(m *Manifest) { m.Provider.Capabilities = []string{"trade"} },
		"research mismatch": func(m *Manifest) { m.Provider.Research = nil },
		"reserved env":      func(m *Manifest) { m.Env = []EnvVar{{Name: "MYSQL_DSN"}} },
		"backend env":       func(m *Manifest) { m.Env = []EnvVar{{Name: "FLOWEYSALES_ANYTHING"}} },
	}
	for name, mutate := range cases {
		m := base
		m.Provider.Capabilities = append([]string{}, base.Provider.Capabilities...)
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: invalid manifest accepted", name)
		}
	}
}

func TestServeIODispatchesConcurrentlyAndReportsErrors(t *testing.T) {
	raw, _ := os.ReadFile(filepath.Join("testdata", "contract", "manifest.json"))
	manifest, _ := ParseManifest(raw)
	release := make(chan struct{})
	p := Plugin{
		Manifest: manifest,
		Quote: func(ctx context.Context, q QuoteParams) (Asset, error) {
			if q.Symbol == "SLOW" {
				<-release
			}
			if q.Symbol == "BAD" {
				return Asset{}, Errorf(CodeInvalidArgument, "bad symbol")
			}
			if q.Symbol == "BOOM" {
				panic("boom")
			}
			if q.Symbol == "PLAIN" {
				return Asset{}, errors.New("upstream down")
			}
			return Asset{Symbol: q.Symbol, Price: 1}, nil
		},
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- ServeIO(context.Background(), p, inR, outW); outW.Close() }()
	lines := bufio.NewScanner(outR)
	write := func(s string) { _, _ = io.WriteString(inW, s+"\n") }
	read := func() Response {
		t.Helper()
		if !lines.Scan() {
			t.Fatal("no response")
		}
		var r struct {
			ID     uint64          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *ResponseError  `json:"error"`
		}
		if err := json.Unmarshal(lines.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		return Response{ID: r.ID, Result: r.Result, Error: r.Error}
	}

	// A slow request must not block a later fast one.
	write(`{"id":1,"method":"quote","params":{"symbol":"SLOW"}}`)
	write(`{"id":2,"method":"quote","params":{"symbol":"FAST"}}`)
	if r := read(); r.ID != 2 || r.Error != nil {
		t.Fatalf("fast request did not overtake slow one: %+v", r)
	}
	close(release)
	if r := read(); r.ID != 1 || r.Error != nil {
		t.Fatalf("slow request: %+v", r)
	}
	checks := []struct {
		req, code string
	}{
		{`{"id":3,"method":"quote","params":{"symbol":"BAD"}}`, CodeInvalidArgument},
		{`{"id":4,"method":"quote","params":{"symbol":"BOOM"}}`, CodeInternal},
		{`{"id":5,"method":"quote","params":{"symbol":"PLAIN"}}`, CodeUpstream},
		{`{"id":6,"method":"history","params":{}}`, CodeUnsupported},
		{`{"id":7,"method":"trade","params":{}}`, CodeUnsupported},
		{`{"id":8,"method":"quote","params":"nope"}`, CodeInvalidArgument},
	}
	for _, c := range checks {
		write(c.req)
		r := read()
		if r.Error == nil || r.Error.Code != c.code {
			t.Errorf("%s: got %+v, want code %s", c.req, r.Error, c.code)
		}
	}
	write(`{"id":9,"method":"describe"}`)
	if r := read(); r.Error != nil || !strings.Contains(string(r.Result.(json.RawMessage)), `"id":"example"`) {
		t.Fatalf("describe: %+v", r)
	}
	inW.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFilterFinancialYearsInclusive(t *testing.T) {
	rows := []QuarterlyFinancial{{PeriodEnd: "2023-12-31"}, {PeriodEnd: "2024-03-31"}, {PeriodEnd: "2025-12-31"}, {PeriodEnd: "bad"}}
	if got := FilterFinancialYears(rows, 2024, 2025); len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got := FilterFinancialYears(rows, 0, 0); len(got) != 4 {
		t.Fatal("zero range must keep all rows")
	}
	if CalendarQuarterLabel("2026-06-30") != "CY2026 Q2" {
		t.Fatal("quarter label")
	}
}
