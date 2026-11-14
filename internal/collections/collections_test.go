package collections_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/collections/collectionstest"
)

func TestMemoryStoreConformance(t *testing.T) {
	collectionstest.Run(t, collections.NewMemoryStore(), 1, 2)
}

func TestValidISIN(t *testing.T) {
	for _, ok := range []string{"US0378331005", "SE0000108656", "US67066G1040"} {
		if !collections.ValidISIN(ok) {
			t.Errorf("%s should be valid", ok)
		}
	}
	for _, bad := range []string{"US0378331006", "0S0378331005", "US037833100", "US03783310055", "us0378331005"} {
		if collections.ValidISIN(bad) {
			t.Errorf("%s should be invalid", bad)
		}
	}
}

func TestNormalizeInstrumentKeepsUnknownsEmpty(t *testing.T) {
	in, err := collections.NormalizeInstrument(collections.Instrument{Provider: " yahoo ", Symbol: "volv-b.st"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Key() != "YAHOO:VOLV-B.ST" || in.Type != "" || in.Currency != "" || in.Country != "" || in.ISIN != "" {
		t.Fatalf("nothing may be guessed from the symbol: %+v", in)
	}
	for name, bad := range map[string]collections.Instrument{
		"no provider": {Symbol: "AAPL"},
		"bad symbol":  {Provider: "YAHOO", Symbol: "AA PL"},
		"bad isin":    {Provider: "YAHOO", Symbol: "AAPL", ISIN: "US0378331006"},
		"bad mic":     {Provider: "YAHOO", Symbol: "AAPL", MIC: "XNASDAQ"},
		"bad ccy":     {Provider: "YAHOO", Symbol: "AAPL", Currency: "DOLLAR"},
		"bad country": {Provider: "YAHOO", Symbol: "AAPL", Country: "USA"},
	} {
		if _, err := collections.NormalizeInstrument(bad); !errors.Is(err, collections.ErrInvalid) {
			t.Errorf("%s: expected ErrInvalid, got %v", name, err)
		}
	}
	if in, _ := collections.NormalizeInstrument(collections.Instrument{Provider: "YAHOO", Symbol: "X", Type: "Warrant"}); in.Type != "other" {
		t.Errorf("unknown asset types become other, got %q", in.Type)
	}
}

func TestCollectionValidation(t *testing.T) {
	for name, c := range map[string]collections.Collection{
		"empty name":   {Name: "   "},
		"long name":    {Name: strings.Repeat("x", collections.MaxNameLen+1)},
		"control char": {Name: "Tech\x00"},
		"unknown kind": {Name: "Tech", Kind: "hedge"},
		"too many tags": {Name: "Tech", Tags: func() []string {
			var tags []string
			for i := 0; i <= collections.MaxTags; i++ {
				tags = append(tags, strings.Repeat("t", i+1))
			}
			return tags
		}()},
	} {
		if _, err := collections.NormalizeCollection(c); !errors.Is(err, collections.ErrInvalid) {
			t.Errorf("%s: expected ErrInvalid, got %v", name, err)
		}
	}
	if _, err := collections.NormalizeCollection(collections.Collection{Name: "Mine", Kind: collections.KindLinked}); !errors.Is(err, collections.ErrUnsupportedKind) {
		t.Errorf("linked views are not offered yet, got %v", err)
	}
	if c, err := collections.NormalizeCollection(collections.Collection{Name: "Notes", Description: "line one\nline two"}); err != nil || !strings.Contains(c.Description, "\n") {
		t.Errorf("descriptions may span lines: %v", err)
	}
}

func TestImportRejectsABadFileWithoutWriting(t *testing.T) {
	store := collections.NewMemoryStore()
	svc := collections.Service{Store: store}
	ctx := context.Background()
	raw := `{"format":"floweysales.collections","version":1,"collections":[
	  {"name":"Good","kind":"watch","members":[{"instrument":{"provider":"YAHOO","symbol":"AAPL"}}]},
	  {"name":"Bad","kind":"watch","members":[{"instrument":{"provider":"YAHOO","symbol":"MSFT"},"weight":10}]}]}`
	var doc collections.Document
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(ctx, 1, doc, collections.ConflictRename); !errors.Is(err, collections.ErrInvalid) {
		t.Fatalf("a weight in a watch collection must fail the import, got %v", err)
	}
	if all, _ := store.ListCollections(ctx, 1); len(all) != 0 {
		t.Fatalf("a rejected file must not create anything: %+v", all)
	}
	for _, bad := range []collections.Document{
		{Format: "other", Version: 1, Collections: doc.Collections},
		{Format: collections.ExportFormat, Version: 2, Collections: doc.Collections},
		{Format: collections.ExportFormat, Version: 1},
	} {
		if _, err := svc.Import(ctx, 1, bad, collections.ConflictRename); !errors.Is(err, collections.ErrInvalid) {
			t.Errorf("expected ErrInvalid for %+v, got %v", bad.Format, err)
		}
	}
	over := 60.0
	heavy := collections.Document{Format: collections.ExportFormat, Version: 1, Collections: []collections.ExportCollection{{Name: "Heavy", Kind: collections.KindModel, Members: []collections.ExportMember{
		{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "A"}, Weight: &over},
		{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "B"}, Weight: &over},
	}}}}
	if _, err := svc.Import(ctx, 1, heavy, collections.ConflictRename); !errors.Is(err, collections.ErrInvalid) {
		t.Errorf("weights above 100%% must fail the import, got %v", err)
	}
}

func TestExportDocumentShape(t *testing.T) {
	svc := collections.Service{Store: collections.NewMemoryStore()}
	ctx := context.Background()
	c, _ := svc.Create(ctx, 7, collections.Collection{Name: "Commodities", Tags: []string{"metals"}})
	for _, sym := range []string{"GC=F", "^GSPC"} {
		if _, err := svc.AddMember(ctx, 7, c.ID, collections.Member{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: sym}}); err != nil {
			t.Fatalf("%s: futures and index symbols belong in collections: %v", sym, err)
		}
	}
	doc, err := svc.Export(ctx, 7, nil, time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	want := `{"format":"floweysales.collections","version":1,"exported_at":"2026-11-14T12:00:00Z","collections":[{"name":"Commodities","kind":"watch","tags":["metals"],"members":[{"instrument":{"provider":"YAHOO","symbol":"GC=F"}},{"instrument":{"provider":"YAHOO","symbol":"^GSPC"}}]}]}`
	if string(raw) != want {
		t.Fatalf("export document changed:\n got %s\nwant %s", raw, want)
	}
}

func TestFreeNameFitsTheLimit(t *testing.T) {
	svc := collections.Service{Store: collections.NewMemoryStore()}
	ctx := context.Background()
	long := strings.Repeat("n", collections.MaxNameLen)
	if _, err := svc.Create(ctx, 1, collections.Collection{Name: long}); err != nil {
		t.Fatal(err)
	}
	doc := collections.Document{Format: collections.ExportFormat, Version: 1, Collections: []collections.ExportCollection{{Name: long, Kind: collections.KindWatch}}}
	res, err := svc.Import(ctx, 1, doc, collections.ConflictRename)
	if err != nil || len([]rune(res[0].SavedAs)) != collections.MaxNameLen || !strings.HasSuffix(res[0].SavedAs, " (2)") {
		t.Fatalf("renamed = %+v, %v", res, err)
	}
}
