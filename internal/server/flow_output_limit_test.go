package server

import (
	"strings"
	"testing"
)

func TestFlowRowsJSONOutputLimit(t *testing.T) {
	t.Cleanup(func() { ConfigureFlowOutputLimit(flowNodeOutputDefault) })
	row := func(n int) map[string]any { return map[string]any{"symbol": "X", "note": strings.Repeat("a", n)} }

	// The leaderboard output from issue 193: 18,883 bytes, which used to fail at 16,000.
	raw, err := flowRowsJSON([]map[string]any{row(18883 - len(`[{"note":"","symbol":"X"}]`))})
	if err != nil || len(raw) != 18883 {
		t.Fatalf("an 18,883 byte output must pass whole: len=%d err=%v", len(raw), err)
	}

	limit := ConfigureFlowOutputLimit(20000)
	if limit != 20000 {
		t.Fatalf("limit = %d", limit)
	}
	base := len(`[{"note":"","symbol":"X"}]`)
	if _, err := flowRowsJSON([]map[string]any{row(limit - base)}); err != nil {
		t.Fatalf("exactly at the limit must pass: %v", err)
	}
	_, err = flowRowsJSON([]map[string]any{row(limit - base + 1)})
	if err == nil || !strings.Contains(err.Error(), "20001 bytes") || !strings.Contains(err.Error(), "FLOW_NODE_OUTPUT_MAX_BYTES") {
		t.Fatalf("one byte over must fail with size and advice, got %v", err)
	}
}

func TestConfigureFlowOutputLimitClamps(t *testing.T) {
	t.Cleanup(func() { ConfigureFlowOutputLimit(flowNodeOutputDefault) })
	if got := ConfigureFlowOutputLimit(1); got != flowNodeOutputMin {
		t.Errorf("low value = %d", got)
	}
	if got := ConfigureFlowOutputLimit(1 << 30); got != flowNodeOutputMax {
		t.Errorf("high value = %d", got)
	}
}

func TestCheckFlowOutputSizeNamesNode(t *testing.T) {
	err := checkFlowOutputSize("subflow 7", flowNodeOutputDefault+1)
	if err == nil || !strings.HasPrefix(err.Error(), "subflow 7 output is") {
		t.Fatalf("got %v", err)
	}
}

func TestRunBudgetNamesTheLargestOutput(t *testing.T) {
	results := map[string]string{"a": "x", "big": strings.Repeat("y", flowRunOutputBudget)}
	err := checkFlowRunBudget(results)
	if err == nil || !strings.Contains(err.Error(), "node big") || !strings.Contains(err.Error(), "run budget") {
		t.Fatalf("got %v", err)
	}
	if err := checkFlowRunBudget(map[string]string{"a": strings.Repeat("y", flowNodeOutputMax)}); err != nil {
		t.Fatalf("one node at the limit is fine: %v", err)
	}
}
