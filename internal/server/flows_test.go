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

func TestFlowGraphAcceptsSafeSubflowReferences(t *testing.T) {
	f := storage.AgentFlow{Name: "Parent research", Graph: json.RawMessage(`{"nodes":[{"id":"trigger","type":"trigger"},{"id":"child","type":"flow","label":"Reusable function","flow_id":123}],"edges":[{"from":"trigger","to":"child"}]}`)}
	_, order, err := validateFlow(f)
	if err != nil || len(order) != 2 {
		t.Fatalf("valid subflow graph rejected: %v %v", order, err)
	}
	f.Graph = json.RawMessage(`{"nodes":[{"id":"trigger","type":"trigger"},{"id":"child","type":"flow","flow_id":0}],"edges":[{"from":"trigger","to":"child"}]}`)
	if _, _, err := validateFlow(f); err == nil {
		t.Fatal("zero subflow ID accepted")
	}
	f.Graph = json.RawMessage(`{"nodes":[{"id":"trigger","type":"trigger"},{"id":"child","type":"flow","flow_id":123,"prompt":"arbitrary code"}],"edges":[{"from":"trigger","to":"child"}]}`)
	if _, _, err := validateFlow(f); err == nil {
		t.Fatal("executable prompt on subflow accepted")
	}
}

func TestBlueprintTypedConstantConnections(t *testing.T) {
	cases := []struct {
		name, graph string
		ok          bool
	}{
		{"text to agent", `{"nodes":[{"id":"start","type":"trigger"},{"id":"value","type":"constant","value_type":"text","value":"Explain AAPL risk"},{"id":"agent","type":"agent","prompt":"Summarize the given research"}],"edges":[{"from":"start","to":"value"},{"from":"value","to":"agent","from_port":"value","to_port":"input"}]}`, true},
		{"number to text rejected", `{"nodes":[{"id":"start","type":"trigger"},{"id":"value","type":"constant","value_type":"number","value":"42"},{"id":"agent","type":"agent","prompt":"Summarize the given research"}],"edges":[{"from":"start","to":"value"},{"from":"value","to":"agent","from_port":"value","to_port":"input"}]}`, false},
		{"invalid number", `{"nodes":[{"id":"start","type":"trigger"},{"id":"value","type":"constant","value_type":"number","value":"hello"}],"edges":[{"from":"start","to":"value"}]}`, false},
		{"invalid boolean", `{"nodes":[{"id":"start","type":"trigger"},{"id":"value","type":"constant","value_type":"boolean","value":"maybe"}],"edges":[{"from":"start","to":"value"}]}`, false},
		{"unknown port", `{"nodes":[{"id":"start","type":"trigger"},{"id":"value","type":"constant","value_type":"text","value":"Research"},{"id":"agent","type":"agent","prompt":"Summarize the given research"}],"edges":[{"from":"start","to":"value"},{"from":"value","to":"agent","from_port":"unknown","to_port":"input"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := storage.AgentFlow{Name: "Typed pins", Graph: json.RawMessage(tc.graph)}
			_, _, err := validateFlow(f)
			if (err == nil) != tc.ok {
				t.Fatalf("expected valid=%v got %v", tc.ok, err)
			}
		})
	}
}
