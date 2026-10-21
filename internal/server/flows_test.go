package server

import (
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func TestFlowGraphValidation(t *testing.T) {
	base := storage.AgentFlow{Name: "Morning research", IntervalMinutes: 15, EventKey: "market.close"}
	cases := []struct {
		name  string
		graph string
		valid bool
	}{
		{"linear", `{"nodes":[{"id":"a","type":"trigger"},{"id":"b","type":"agent","prompt":"Research ticker prices"}],"edges":[{"from":"a","to":"b"}]}`, true},
		{"two agents", `{"nodes":[{"id":"a","type":"trigger"},{"id":"b","type":"agent","prompt":"Research ticker prices"},{"id":"c","type":"agent","prompt":"Review the output"}],"edges":[{"from":"a","to":"b"},{"from":"b","to":"c"}]}`, true},
		{"cycle", `{"nodes":[{"id":"a","type":"trigger"},{"id":"b","type":"agent","prompt":"Research ticker prices"}],"edges":[{"from":"a","to":"b"},{"from":"b","to":"a"}]}`, false},
		{"missing edge", `{"nodes":[{"id":"a","type":"trigger"},{"id":"b","type":"agent","prompt":"Research ticker prices"}],"edges":[]}`, false},
		{"unknown executor", `{"nodes":[{"id":"a","type":"trigger"},{"id":"b","type":"shell","prompt":"Execute arbitrary command"}],"edges":[{"from":"a","to":"b"}]}`, false},
		{"missing prompt", `{"nodes":[{"id":"a","type":"trigger"},{"id":"b","type":"agent"}],"edges":[{"from":"a","to":"b"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.Graph = json.RawMessage(tc.graph)
			_, order, err := validateFlow(f)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, order=%v, err=%v", tc.valid, order, err)
			}
		})
	}
	f := base
	f.IntervalMinutes = 1
	f.Graph = json.RawMessage(cases[0].graph)
	if _, _, err := validateFlow(f); err == nil {
		t.Fatal("schedule below five minutes accepted")
	}
	f = base
	f.EventKey = "not allowed!"
	f.Graph = json.RawMessage(cases[0].graph)
	if _, _, err := validateFlow(f); err == nil {
		t.Fatal("invalid event key accepted")
	}
	f = base
	f.Graph = json.RawMessage(cases[0].graph)
	f.Name = strings.Repeat("a", 121)
	if _, _, err := validateFlow(f); err == nil {
		t.Fatal("oversized name accepted")
	}
}
