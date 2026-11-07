package server

import (
	"strings"

	"floweysales/internal/sources"
)

// flowOutputMax is the bound on one node's output (see the node limits).
const flowOutputMax = 6000

// renderFlowCitations turns the [Sn] markers of an agent node's Markdown
// answer into links and appends the list of the sources it cites, from the
// sources the step persisted, so the report shows where each statement came
// from in every place its text goes (run history, notifications, downstream
// nodes). The whole output, list included, stays within the node output limit:
// the answer is shortened first.
func renderFlowCitations(answer string, list []sources.Source) string {
	if len(list) == 0 {
		if len(answer) > flowOutputMax {
			return strings.ToValidUTF8(answer[:flowOutputMax], "")
		}
		return answer
	}
	body := answer
	if len(body) > flowOutputMax {
		body = strings.ToValidUTF8(body[:flowOutputMax], "")
	}
	for i := 0; i < 40; i++ {
		out := sources.RenderCitations(body, list)
		if len(out) <= flowOutputMax {
			return out
		}
		over := len(out) - flowOutputMax
		if len(body) <= 1 {
			break
		}
		body = strings.ToValidUTF8(body[:max(1, len(body)*(flowOutputMax-200)/len(out)-over/100)], "")
	}
	return strings.ToValidUTF8(sources.RenderCitations(body, list)[:flowOutputMax], "")
}
