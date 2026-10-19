package market

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPublicAPIInventoryMatchesProviderWhitelist(t *testing.T) {
	raw, err := os.ReadFile("../../site/api-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Providers []struct {
			Provider  string `json:"provider"`
			Endpoints []struct {
				Operation string `json:"operation"`
				Status    string `json:"status"`
				Path      string `json:"path"`
			} `json:"endpoints"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	actual := map[string]bool{}
	for _, entry := range ResearchOperations() {
		actual[entry.Provider+"/"+entry.Operation] = true
	}
	documented := map[string]bool{}
	for _, p := range catalog.Providers {
		for _, v := range p.Endpoints {
			if v.Operation == "" {
				continue
			}
			key := p.Provider + "/" + v.Operation
			if v.Status != "implemented" {
				t.Errorf("whitelisted operation %s must be marked implemented", key)
			}
			if documented[key] {
				t.Errorf("duplicate documented operation %s", key)
			}
			documented[key] = true
			if !actual[key] {
				t.Errorf("docs claim unsupported operation %s", key)
			}
		}
	}
	for key := range actual {
		if !documented[key] {
			t.Errorf("new operation missing from help page: %s", key)
		}
	}
}
