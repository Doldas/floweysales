package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

// The investor playbooks are built in the frontend (investor-nodes.ts); its
// spec writes them to this fixture so the server validator checks the exact
// graphs users start from.
func TestInvestorPlaybooksPassServerValidation(t *testing.T) {
	raw, err := os.ReadFile("testdata/investor-playbooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var playbooks map[string]struct {
		Name            string          `json:"name"`
		IntervalMinutes int             `json:"intervalMinutes"`
		Graph           json.RawMessage `json:"graph"`
	}
	if err := json.Unmarshal(raw, &playbooks); err != nil {
		t.Fatal(err)
	}
	if len(playbooks) < 10 {
		t.Fatalf("expected admin and user variants of every playbook, got %d", len(playbooks))
	}
	for key, p := range playbooks {
		f := storage.AgentFlow{Name: p.Name, Graph: p.Graph, IntervalMinutes: p.IntervalMinutes}
		g, _, err := validateFlow(f)
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		if strings.HasSuffix(key, "/user") && !flowAllowedForNonAdmin(g) {
			t.Errorf("%s: the user variant must be runnable by ordinary users", key)
		}
		for _, n := range g.Nodes {
			if err := validateFlowAgentTools(n); err != nil {
				t.Errorf("%s: %v", key, err)
			}
		}
	}
}
