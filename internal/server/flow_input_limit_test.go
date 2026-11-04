package server

import (
	"strings"
	"testing"
)

func TestCheckFlowAgentInput(t *testing.T) {
	if err := checkFlowAgentInput("a1", flowAgentInputMax); err != nil {
		t.Fatalf("exactly at the limit must pass: %v", err)
	}
	err := checkFlowAgentInput("a1", flowAgentInputMax+1)
	if err == nil || !strings.Contains(err.Error(), "a1") || !strings.Contains(err.Error(), "screen or report_table") {
		t.Fatalf("over the limit must name the node and the fix, got %v", err)
	}
}

func TestTruncateNotifiedSaysSo(t *testing.T) {
	short := "hello"
	if got := truncateNotified(short, 100, 7); got != short {
		t.Fatalf("a short body must be unchanged, got %q", got)
	}
	long := strings.Repeat("é", 500)
	got := truncateNotified(long, 300, 42)
	if len(got) > 300 {
		t.Fatalf("result is %d bytes, above the cap", len(got))
	}
	if !strings.Contains(got, "Shortened") || !strings.Contains(got, "run #42") {
		t.Fatalf("a cut body must say so and name the run, got %q", got)
	}
}
