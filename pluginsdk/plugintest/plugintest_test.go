package plugintest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/Doldas/floweysales/pluginsdk"
	"github.com/Doldas/floweysales/pluginsdk/research"
)

func fixture(t *testing.T) sdk.Manifest {
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "contract", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := sdk.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func conforming(m sdk.Manifest) sdk.Plugin {
	return sdk.Plugin{
		Manifest: m,
		Quote:    func(context.Context, sdk.QuoteParams) (sdk.Asset, error) { return sdk.Asset{Price: 1}, nil },
		History: func(context.Context, sdk.HistoryParams) (sdk.HistorySeries, error) {
			return sdk.HistorySeries{}, nil
		},
		Research: func(_ context.Context, p sdk.ResearchParams) (sdk.ResearchResult, error) {
			op, err := research.Find(m, p.Operation)
			if err != nil {
				return sdk.ResearchResult{}, err
			}
			return research.Result(m.Provider.ID, op, p.Symbol, "test", nil, ""), nil
		},
	}
}

func TestConformingPluginPasses(t *testing.T) {
	Conformance(t, conforming(fixture(t)))
}

func TestCallReturnsCodedErrors(t *testing.T) {
	c := Start(t, conforming(fixture(t)))
	var asset sdk.Asset
	if err := c.Call(sdk.MethodQuote, sdk.QuoteParams{Symbol: "X"}, &asset); err != nil || asset.Price != 1 {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	if code := errorCode(c.Call(sdk.MethodSearch, sdk.SearchParams{Query: "x"}, nil)); code != sdk.CodeUnsupported {
		t.Fatalf("search: %s", code)
	}
}

// fakeT records failures instead of failing the real test.
type fakeT struct {
	*testing.T
	fail func(string)
}

func (f *fakeT) Errorf(format string, args ...any) { f.fail(fmt.Sprintf(format, args...)) }
func (f *fakeT) Fatalf(format string, args ...any) { f.fail(fmt.Sprintf(format, args...)) }

func TestConformanceCatchesMismatches(t *testing.T) {
	m := fixture(t)
	missing := conforming(m)
	missing.History = nil
	extra := conforming(m)
	extra.Search = func(context.Context, sdk.SearchParams) ([]sdk.SearchResult, error) { return nil, nil }
	lax := conforming(m)
	lax.Research = func(context.Context, sdk.ResearchParams) (sdk.ResearchResult, error) {
		return sdk.ResearchResult{}, nil
	}
	for name, tc := range map[string]struct {
		p    sdk.Plugin
		want string
	}{
		"declared without handler":   {missing, "has no handler"},
		"handler without capability": {extra, "not declared"},
		"research not allowlisted":   {lax, "undeclared research operation"},
	} {
		t.Run(name, func(t *testing.T) {
			var failures []string
			ft := &fakeT{T: t, fail: func(msg string) { failures = append(failures, msg) }}
			conformance(ft, tc.p)
			if !strings.Contains(strings.Join(failures, "\n"), tc.want) {
				t.Fatalf("want a failure containing %q, got %q", tc.want, failures)
			}
		})
	}
}
