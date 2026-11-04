package server

import "fmt"

// flowAgentInputMax bounds the previous-node text pasted into an agent prompt.
// Cutting JSON mid-value would mislead the model, so an oversized input fails instead.
const flowAgentInputMax = 64 << 10

func checkFlowAgentInput(node string, size int) error {
	if size <= flowAgentInputMax {
		return nil
	}
	return fmt.Errorf("agent node %s would receive %d bytes of previous output, above the %d byte input limit; add a screen or report_table node before the agent to keep only the rows and columns it needs", node, size, flowAgentInputMax)
}

// truncateNotified shortens a notification body and says so, with a pointer to the full output.
func truncateNotified(body string, max int, runID int64) string {
	if len(body) <= max {
		return body
	}
	where := "the flow's run history"
	if runID > 0 {
		where = fmt.Sprintf("run #%d in the flow's run history", runID)
	}
	notice := fmt.Sprintf("\n\n[Shortened: this message shows the first part of %d characters. The full output is in %s.]", len(body), where)
	return truncateText(body, max-len(notice)) + notice
}
