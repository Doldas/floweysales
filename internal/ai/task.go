package ai

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"floweysales/internal/sources"
)

// A task is a run with a checklist of required deliverables, an evidence
// ledger of every tool call, and checkpoints a failed or cancelled run can
// be resumed from. The agent may only report success when every deliverable
// is reported done and the evidence ledger is not empty.

const (
	// MaxDeliverables bounds the checklist of one task.
	MaxDeliverables = 12
	// maxCompletionNudges is how often a final answer with missing
	// deliverables is sent back to the model before the run ends incomplete.
	maxCompletionNudges = 2
	// ledgerPreviewBytes is how much of a tool result the ledger keeps.
	ledgerPreviewBytes = 600
)

// Task statuses.
const (
	TaskRunning    = "running"
	TaskComplete   = "complete"
	TaskIncomplete = "incomplete" // the run ended but deliverables are missing
	TaskFailed     = "failed"
	TaskCancelled  = "cancelled"
)

// Deliverable statuses.
const (
	DeliverablePending = "pending"
	DeliverableDone    = "done"
	DeliverableMissing = "missing"
)

// Deliverable is one required part of the answer.
type Deliverable struct {
	Title  string `json:"title"`
	Status string `json:"status"`
	// Note is the model's reason when a deliverable is missing.
	Note string `json:"note,omitempty"`
}

// LedgerEntry records one tool call and where its answer came from.
type LedgerEntry struct {
	Seq       int       `json:"seq"`
	Tool      string    `json:"tool"`
	Arguments string    `json:"arguments,omitempty"`
	Preview   string    `json:"preview,omitempty"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

// Checkpoint is everything needed to continue a run: the conversation (the
// system prompt is rebuilt), the evidence gathered so far and the budgets
// already spent.
type Checkpoint struct {
	Messages     []Message     `json:"messages"`
	Ledger       []LedgerEntry `json:"ledger,omitempty"`
	Deliverables []Deliverable `json:"deliverables,omitempty"`
	Steps        int           `json:"steps"`
	ToolCalls    int           `json:"tool_calls"`
	Status       string        `json:"status"`
	// NoTools records that the run had tools disabled, so a resume keeps the
	// same rules for what counts as evidence.
	NoTools bool `json:"no_tools,omitempty"`
	// Sources, Audit and Removed are what the run retrieved and what the
	// citation check found in the answer so far; Evidence is the policy it ran
	// under. A resume restores them, so [S3] keeps its meaning.
	Sources  []sources.Source  `json:"sources,omitempty"`
	Audit    []sources.Finding `json:"audit,omitempty"`
	Removed  []sources.Finding `json:"removed,omitempty"`
	Claims   []sources.Claim   `json:"claims,omitempty"`
	Evidence *EvidencePolicy   `json:"evidence,omitempty"`
}

// maxCheckpointSources bounds the sources a checkpoint keeps.
const maxCheckpointSources = 60

// NewDeliverables builds a pending checklist from titles, dropping blanks and
// duplicates and capping its length.
func NewDeliverables(titles []string) []Deliverable {
	seen := map[string]bool{}
	var out []Deliverable
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] || len(out) >= MaxDeliverables {
			continue
		}
		if len(t) > 200 {
			t = t[:200]
		}
		seen[strings.ToLower(t)] = true
		out = append(out, Deliverable{Title: t, Status: DeliverablePending})
	}
	return out
}

var deliverableLine = regexp.MustCompile(`(?im)^\s*[-*]?\s*\[(done|missing)\]\s*(.+?)\s*$`)

// reportFormat is appended to the system prompt of a task run.
func reportFormat(ds []Deliverable) string {
	var b strings.Builder
	b.WriteString("\n\nThis task has required deliverables:\n")
	for i, d := range ds {
		fmt.Fprintf(&b, "%d. %s\n", i+1, d.Title)
	}
	b.WriteString("End your final answer with one line per deliverable, exactly `[done] <deliverable>` when you finished it with sourced data, or `[missing] <deliverable> - <reason>` when you could not. Never mark a deliverable done that rests on guesses.")
	return b.String()
}

// ApplyReport reads the `[done]`/`[missing]` lines of a final answer into the
// checklist and returns the updated copy. A deliverable the answer does not
// mention stays pending, so it counts as missing.
func ApplyReport(ds []Deliverable, answer string) []Deliverable {
	out := append([]Deliverable(nil), ds...)
	for _, m := range deliverableLine.FindAllStringSubmatch(answer, -1) {
		status, rest := strings.ToLower(m[1]), m[2]
		title, note := rest, ""
		if i := strings.Index(rest, " - "); i >= 0 {
			title, note = rest[:i], strings.TrimSpace(rest[i+3:])
		}
		for i := range out {
			if matchesTitle(out[i].Title, title) {
				out[i].Status, out[i].Note = status, note
			}
		}
	}
	return out
}

func matchesTitle(want, got string) bool {
	w, g := strings.ToLower(strings.TrimSpace(want)), strings.ToLower(strings.TrimSpace(got))
	return w != "" && g != "" && (w == g || strings.HasPrefix(g, w) || strings.HasPrefix(w, g))
}

// OutstandingDeliverables lists the titles not reported done.
func OutstandingDeliverables(ds []Deliverable) []string {
	var out []string
	for _, d := range ds {
		if d.Status != DeliverableDone {
			out = append(out, d.Title)
		}
	}
	return out
}

// Complete reports whether a task may claim success: every deliverable is
// done, and, when the run had tools, at least one tool call backs it.
func Complete(ds []Deliverable, ledger []LedgerEntry, toolsAvailable bool) bool {
	if len(OutstandingDeliverables(ds)) > 0 {
		return false
	}
	if toolsAvailable && len(ledger) == 0 {
		return false
	}
	return true
}
