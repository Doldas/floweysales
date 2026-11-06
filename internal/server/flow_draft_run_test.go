package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func TestSimulatedDraftTestSkipsEverySideEffect(t *testing.T) {
	e := &Engine{} // No storage: a simulated effect must not touch it.
	applied := false
	apply := func(string) (string, error) { applied = true; return "", nil }
	ctx := withFlowSimulation(context.Background())
	for _, n := range []flowNode{
		{ID: "w", Type: "workspace_write", Value: "/a.md"},
		{ID: "g", Type: "workspace_write", Value: "/a.md", Approval: approvalRequired},
		{ID: "e", Type: "emit_event", Event: "market.close"},
		{ID: "n", Type: "notify", Channel: "email"},
	} {
		summary, err := e.performSideEffect(ctx, storage.AgentFlow{ID: 1}, n, 0, "payload", "text/plain", apply)
		if err != nil || !strings.HasPrefix(summary, "Simulated by the draft test") {
			t.Fatalf("%s: %q %v", n.ID, summary, err)
		}
	}
	if applied {
		t.Fatal("a simulated draft test performed a side effect")
	}
	if flowSimulated(context.Background()) {
		t.Fatal("an ordinary run must not be simulated")
	}
}

func TestDraftTestSnapshotHashAndSideEffectListing(t *testing.T) {
	graph := json.RawMessage(`{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"w","type":"workspace_write","label":"Write","value":"/r.md","content_type":"text/markdown"},{"id":"e","type":"emit_event","event":"a.b"}],"edges":[{"from":"t","to":"w"},{"from":"w","to":"e"}]}`)
	hash := flowSnapshotHash(graph)
	if len(hash) != 64 || hash != flowSnapshotHash(graph) || hash == flowSnapshotHash(json.RawMessage(`{"version":2}`)) {
		t.Fatalf("hash should be a stable SHA-256 of the exact snapshot: %s", hash)
	}
	g, _, err := validateFlow(storage.AgentFlow{Name: "Side effects", Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	effects := flowSideEffectNodes(g)
	if len(effects) != 2 || effects[0]["capability"] != capabilityWorkspaceWrite || effects[1]["capability"] != capabilityEventsEmit {
		t.Fatalf("side effects: %v", effects)
	}
}

func TestScratchScopeBelongsToOneTester(t *testing.T) {
	for _, user := range []int64{0, 1, 42} {
		id := storage.ScratchFlowID(user)
		owner, scratch := storage.ScratchFlowOwner(id)
		if id >= 0 || !scratch || owner != user {
			t.Fatalf("user %d: id %d owner %d scratch %v", user, id, owner, scratch)
		}
	}
	if _, scratch := storage.ScratchFlowOwner(5); scratch {
		t.Fatal("a stored flow is not a scratch scope")
	}
	if storage.ScratchFlowID(1) == storage.ScratchFlowID(2) {
		t.Fatal("testers must not share a history")
	}
}
