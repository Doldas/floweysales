package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// collectionFlowEngine has owner 5's model collection "Chips" (NVIDIA on
// FINN, Volvo and the S&P 500 index on YAHOO) and owner 6's "Secret". The
// flows it runs belong to owner 5.
func collectionFlowEngine(t *testing.T) (*Engine, *collNewsBackend, collections.Collection, collections.Collection) {
	t.Helper()
	e, b := collNewsEngine()
	e.flowOwner = func(context.Context, int64) (int64, error) { return 5, nil }
	ctx := context.Background()
	svc, _ := e.collectionService()
	mine, err := svc.Create(ctx, 5, collections.Collection{Name: "Chips", Kind: collections.KindModel, Tags: []string{"AI"}})
	if err != nil {
		t.Fatal(err)
	}
	weight := 40.0
	for _, m := range []collections.Member{
		{Instrument: collections.Instrument{Provider: "FINN", Symbol: "NVDA", Name: "NVIDIA Corporation"}, Note: "watch margins", Tags: []string{"core"}, Weight: &weight},
		{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "VOLV-B.ST", Name: "AB Volvo", Currency: "SEK"}},
		{Instrument: collections.Instrument{Provider: "YAHOO", Symbol: "^GSPC", Name: "S&P 500"}},
	} {
		if _, err := svc.AddMember(ctx, 5, mine.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	theirs, err := svc.Create(ctx, 6, collections.Collection{Name: "Secret", Kind: collections.KindWatch})
	if err != nil {
		t.Fatal(err)
	}
	return e, b, mine, theirs
}

func flowRowsOf(t *testing.T, out string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return rows
}

func TestCollectionNodeReadsTheFlowOwnersCollection(t *testing.T) {
	e, _, mine, theirs := collectionFlowEngine(t)
	ctx := context.Background()
	f := storage.AgentFlow{ID: 9}

	out, note, err := e.runInvestorNode(ctx, f, flowNode{ID: "c", Type: "collection", Collection: "chips"}, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := flowRowsOf(t, out)
	if len(rows) != 2 {
		t.Fatalf("the index is left out, the rest passed on: %s", out)
	}
	nvda, volvo := rows[0], rows[1]
	if nvda["symbol"] == "VOLV-B.ST" {
		nvda, volvo = volvo, nvda
	}
	if nvda["provider"] != "FINN" || nvda["weight_pct"] != 40.0 || nvda["note"] != "watch margins" || nvda["tags"] != "core" || nvda["collection"] != "Chips" {
		t.Fatalf("member row: %v", nvda)
	}
	if volvo["provider"] != "YAHOO" || volvo["currency"] != "SEK" || volvo["weight_pct"] != nil {
		t.Fatalf("member row: %v", volvo)
	}
	if p, _ := nvda["provenance"].(map[string]any); p["kind"] != "assumed" || !strings.Contains(p["note"].(string), "hypothetical") {
		t.Fatalf("a member is the user's choice: %v", nvda["provenance"])
	}
	if !strings.Contains(note, "Left out ^GSPC") {
		t.Fatalf("note: %q", note)
	}

	// The limit passes on fewer, and says so.
	out, note, err = e.runInvestorNode(ctx, f, flowNode{ID: "c", Type: "collection", Collection: strconv.FormatInt(mine.ID, 10), Limit: 1}, 0, "")
	if err != nil || len(flowRowsOf(t, out)) != 1 || !strings.Contains(note, "1 of 2 instruments passed on") {
		t.Fatalf("limit: %v %q %s", err, note, out)
	}

	// Another user's collection is not found, by name or by id.
	for _, ref := range []string{"Secret", strconv.FormatInt(theirs.ID, 10)} {
		if _, _, err := e.runInvestorNode(ctx, f, flowNode{ID: "c", Type: "collection", Collection: ref}, 0, ""); err == nil || !strings.Contains(err.Error(), "the flow owner has no collection") {
			t.Fatalf("%s must not be readable: %v", ref, err)
		}
	}
}

func TestCollectionNewsNodeKeepsRelevanceAndReasons(t *testing.T) {
	e, b, _, _ := collectionFlowEngine(t)
	out, note, err := e.runInvestorNode(context.Background(), storage.AgentFlow{ID: 9}, flowNode{ID: "n", Type: "collection_news", Collection: "Chips", Limit: 2}, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := flowRowsOf(t, out)
	if len(rows) != 2 {
		t.Fatalf("limit: %s", out)
	}
	top := rows[0]
	if top["title"] != "Nvidia beats estimates" || top["relevance"] != "direct" || top["provider"] != "FINN" || top["date_status"] != "published" ||
		!strings.Contains(top["reasons"].(string), `Names "NVIDIA"`) {
		t.Fatalf("top row: %v", top)
	}
	if p, _ := top["provenance"].(map[string]any); p["url"] != "https://news.example/nvda-beats" {
		t.Fatalf("a headline links its source: %v", top["provenance"])
	}
	if !strings.Contains(note, "2 on a provider without news") {
		t.Fatalf("Volvo and the index are on YAHOO, which has no news: %q", note)
	}
	if b.newsCount("VOLV-B.ST") != 0 {
		t.Fatal("another provider's headlines must never be borrowed")
	}
}

// listingBackend serves bars only for the listing it knows: NVDA on FINN and
// VOLV-B.ST on YAHOO, recording which provider was asked.
type listingBackend struct {
	investorBackend
	mu    sync.Mutex
	asked []string
}

func (b *listingBackend) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	b.mu.Lock()
	b.asked = append(b.asked, string(p)+":"+symbol)
	b.mu.Unlock()
	if (symbol == "NVDA" && p == "FINN") || (symbol == "VOLV-B.ST" && p == "YAHOO") {
		return b.investorBackend.History(ctx, p, symbol, rng)
	}
	return market.HistorySeries{}, fmt.Errorf("%s does not list %s", p, symbol)
}

func TestDataNodesAskEachCollectionMembersOwnProvider(t *testing.T) {
	e := investorEngine(t)
	b := &listingBackend{investorBackend: investorBackend{now: time.Now().UTC()}}
	e.providers = b
	input := `[{"symbol":"NVDA","provider":"FINN"},{"symbol":"VOLV-B.ST","provider":"YAHOO"}]`
	out, _, err := e.runInvestorNode(context.Background(), storage.AgentFlow{ID: 9}, flowNode{ID: "q", Type: "watchlist_quotes"}, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	rows := flowRowsOf(t, out)
	for _, row := range rows {
		if row["error"] != nil || row["price"] == nil {
			t.Fatalf("each member from its own provider: %v (asked %v)", row, b.asked)
		}
		if p, _ := row["provenance"].(map[string]any); p["provider"] != row["provider"] {
			t.Fatalf("the source names the member's provider: %v", row)
		}
	}
	if rows[0]["provider"] != "FINN" || rows[1]["provider"] != "YAHOO" {
		t.Fatalf("%v", rows)
	}

	// A provider chosen on the node still wins: that is the user's decision.
	b.asked = nil
	out, _, err = e.runInvestorNode(context.Background(), storage.AgentFlow{ID: 9}, flowNode{ID: "q", Type: "watchlist_quotes", Provider: "YAHOO"}, 0, input)
	if err != nil {
		t.Fatal(err)
	}
	rows = flowRowsOf(t, out)
	if !strings.Contains(fmt.Sprint(rows[0]["error"]), "YAHOO does not list NVDA") || rows[1]["error"] != nil {
		t.Fatalf("%v", rows)
	}
}

func TestFlowTargetsKeepEachListing(t *testing.T) {
	got, err := flowTargets("", `[{"symbol":"abb","provider":"finn"},{"symbol":"ABB","provider":"YAHOO"},{"symbol":"ABB","provider":"FINN"},{"symbol":"MSFT"}]`)
	if err != nil {
		t.Fatal(err)
	}
	want := []flowTarget{{Symbol: "ABB", Provider: "FINN"}, {Symbol: "ABB", Provider: "YAHOO"}, {Symbol: "MSFT"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if symbols, _ := flowSymbols("", `[{"symbol":"ABB","provider":"FINN"},{"symbol":"ABB","provider":"YAHOO"}]`); len(symbols) != 1 {
		t.Fatalf("a ticker-only source sees one ticker: %v", symbols)
	}
	if _, err := flowTargets("", `[{"symbol":"ABB","provider":"not a provider!"}]`); err == nil {
		t.Fatal("an invalid provider is an error, not the default")
	}
}

func TestCollectionNodeValidation(t *testing.T) {
	ok := []string{
		`{"id":"n","type":"collection","label":"C","collection":"Chips"}`,
		`{"id":"n","type":"collection_news","label":"C","collection":"12","limit":50}`,
	}
	bad := map[string]string{
		"no collection":         `{"id":"n","type":"collection","label":"C"}`,
		"a watchlist":           `{"id":"n","type":"collection","label":"C","collection":"Chips","symbols":"AAPL"}`,
		"a provider":            `{"id":"n","type":"collection_news","label":"C","collection":"Chips","provider":"YAHOO"}`,
		"too many headlines":    `{"id":"n","type":"collection_news","label":"C","collection":"Chips","limit":51}`,
		"collection elsewhere":  `{"id":"n","type":"watchlist_quotes","label":"Q","symbols":"AAPL","collection":"Chips"}`,
		"collection on a value": `{"id":"n","type":"constant","label":"V","value":"x","collection":"Chips"}`,
	}
	wrap := func(node string) string {
		return `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},` + node + `],"edges":[{"from":"start","to":"n"}]}`
	}
	for _, node := range ok {
		g, _, err := validateFlow(storage.AgentFlow{Name: "Collection", Graph: json.RawMessage(wrap(node))})
		if err != nil || !flowAllowedForNonAdmin(g) {
			t.Errorf("%s: %v", node, err)
		}
	}
	for name, node := range bad {
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Collection", Graph: json.RawMessage(wrap(node))}); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}
