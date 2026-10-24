package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"floweysales/internal/storage"
)

func TestFlowFailureIsAttributedToTopLevelNode(t *testing.T) {
	graph := `{"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"clean","type":"transform","label":"Trim","transform":"trim"},{"id":"restore","type":"load_result","label":"Restore summary","variable":"summary"}],"edges":[{"from":"start","to":"clean"},{"from":"clean","to":"restore"}]}`
	f := storage.AgentFlow{ID: 1, Name: "Missing variable", Graph: json.RawMessage(graph)}
	if _, _, err := validateFlow(f); err != nil {
		t.Fatalf("fixture must pass validation so the failure happens at runtime: %v", err)
	}
	_, err := (&Engine{}).executeFlowNestedWithInput(context.Background(), f, map[int64]bool{}, 0, " input ")
	var nodeErr *flowNodeError
	if !errors.As(err, &nodeErr) {
		t.Fatalf("expected node-attributed error, got %T %v", err, err)
	}
	if nodeErr.NodeID != "restore" || nodeErr.Label != "Restore summary" {
		t.Fatalf("attributed to %q (%q), want restore", nodeErr.NodeID, nodeErr.Label)
	}
	if nodeErr.Attempts != nil {
		t.Fatalf("non-retryable node must not report attempts, got %d", *nodeErr.Attempts)
	}
	if nodeErr.DurationMS < 0 {
		t.Fatalf("negative duration %d", nodeErr.DurationMS)
	}
	if err.Error() != "node restore: variable summary has not been saved" {
		t.Fatalf("wrapping must preserve the original message, got %q", err.Error())
	}
}

func TestFlowFailureNotAttributedInsideSubflowsOrOnCancel(t *testing.T) {
	graph := `{"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"restore","type":"load_result","label":"Restore","variable":"summary"}],"edges":[{"from":"start","to":"restore"}]}`
	f := storage.AgentFlow{ID: 2, Name: "Nested", Graph: json.RawMessage(graph)}
	var nodeErr *flowNodeError
	if _, err := (&Engine{}).executeFlowNestedWithInput(context.Background(), f, map[int64]bool{}, 1, ""); err == nil || errors.As(err, &nodeErr) {
		t.Fatalf("nested flow errors must stay unattributed so the parent node owns them, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Engine{}).executeFlowNestedWithInput(ctx, f, map[int64]bool{}, 0, ""); !errors.Is(err, context.Canceled) || errors.As(err, &nodeErr) {
		t.Fatalf("cancelled runs must not blame a node, got %v", err)
	}
}
