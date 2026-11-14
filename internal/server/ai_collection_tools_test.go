package server

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/collections"
	"floweysales/internal/identity"
)

func TestCollectionToolsReadOnlyTheCallersCollections(t *testing.T) {
	e, _ := collNewsEngine()
	e.authService = &identity.Service{}
	ctx := context.Background()
	svc, _ := e.collectionService()
	mine, err := svc.Create(ctx, 5, collections.Collection{Name: "Chips", Kind: collections.KindModel, Tags: []string{"AI"}})
	if err != nil {
		t.Fatal(err)
	}
	weight := 40.0
	if _, err := svc.AddMember(ctx, 5, mine.ID, collections.Member{Instrument: collections.Instrument{Provider: "FINN", Symbol: "NVDA", Name: "NVIDIA Corporation"}, Note: "watch margins", Weight: &weight}); err != nil {
		t.Fatal(err)
	}
	theirs, err := svc.Create(ctx, 6, collections.Collection{Name: "Secret", Kind: collections.KindWatch})
	if err != nil {
		t.Fatal(err)
	}
	user := withAgentUser(ctx, identity.User{ID: 5, Role: identity.RoleUser})

	list, err := runTool(t, e, user, "list_collections", "")
	if err != nil {
		t.Fatal(err)
	}
	rows := list["collections"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["name"] != "Chips" || !strings.Contains(list["notice"].(string), "cannot change collections or trade") {
		t.Fatalf("only the caller's collections: %v", list)
	}

	got, err := runTool(t, e, user, "get_collection", `{"collection":"chips"}`)
	if err != nil {
		t.Fatal(err)
	}
	members := got["members"].([]any)
	if len(members) != 1 {
		t.Fatalf("%v", got)
	}
	m := members[0].(map[string]any)
	if m["symbol"] != "NVDA" || m["weight_pct"] != 40.0 || m["note"] != "watch margins" || m["profile_status"] != "missing" {
		t.Fatalf("member row: %v", m)
	}
	// The id works too, given as a number.
	if _, err := runTool(t, e, user, "get_collection", `{"collection":`+strconv.FormatInt(mine.ID, 10)+`}`); err != nil {
		t.Fatal(err)
	}
	// Another user's collection is not found, by id or by name.
	if _, err := runTool(t, e, user, "get_collection", `{"collection":"`+strconv.FormatInt(theirs.ID, 10)+`"}`); err == nil || !strings.Contains(err.Error(), "no collection with id") {
		t.Fatalf("another user's collection must not be readable: %v", err)
	}
	if _, err := runTool(t, e, user, "get_collection_news", `{"collection":"Secret"}`); err == nil || !strings.Contains(err.Error(), "no collection named") {
		t.Fatalf("another user's collection must not be readable: %v", err)
	}

	news, err := runTool(t, e, user, "get_collection_news", `{"collection":"Chips","limit":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if items := news["items"].([]any); len(items) != 2 || items[0].(map[string]any)["relevance"] != "direct" {
		t.Fatalf("news: %v", news)
	}

	// A run that acts for nobody, such as a flow's agent node, gets a refusal.
	for _, name := range ai.CollectionToolNames {
		if _, err := runTool(t, e, ctx, name, `{"collection":"Chips"}`); err == nil || !strings.Contains(err.Error(), "signed-in chat session") {
			t.Fatalf("%s without a user: %v", name, err)
		}
		// The MCP server's HTTP data source has no collections: a clear error.
		if _, err := ai.Execute(ctx, nil, name, `{"collection":"Chips"}`); err == nil || !strings.Contains(err.Error(), "unavailable for this data source") {
			t.Fatalf("%s without a source: %v", name, err)
		}
	}
	if _, err := runTool(t, e, user, "get_collection", `{}`); err == nil {
		t.Fatal("a collection is required")
	}
}

func TestCollectionToolsAreOwnDataReadOnlyAndNotGrantableToFlows(t *testing.T) {
	for _, name := range ai.CollectionToolNames {
		if !slices.Contains(ai.ToolNames(), name) {
			t.Errorf("%s is not advertised", name)
		}
		if agentToolClass[name] != toolOwn {
			t.Errorf("%s must read only the caller's own data", name)
		}
		if !slices.Contains(userAgentTools(), name) {
			t.Errorf("%s must be available to ordinary users", name)
		}
		if slices.Contains(flowAgentToolCatalog, name) {
			t.Errorf("%s belongs to one person and must not be grantable to a flow's agent", name)
		}
		if !strings.HasPrefix(name, "list_") && !strings.HasPrefix(name, "get_") {
			t.Errorf("%s: collection tools only read", name)
		}
	}
	if !meteredAgentTools["get_collection_news"] {
		t.Error("collection news asks providers and must be metered")
	}
}
