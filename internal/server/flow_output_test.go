package server

import (
	"encoding/json"
	"testing"

	"floweysales/internal/storage"
)

func TestFlowAgentOutputGraphAndFormats(t *testing.T) {
	cases := []struct {
		name  string
		graph string
		valid bool
	}{
		{
			name:  "agent to output",
			graph: `{"nodes":[{"id":"start","type":"trigger"},{"id":"agent","type":"agent","prompt":"Research the asset thoroughly"},{"id":"display","type":"output","label":"Show result"}],"edges":[{"from":"start","to":"agent"},{"from":"agent","to":"display"}]}`,
			valid: true,
		},
		{
			name:  "agent json to transform",
			graph: `{"nodes":[{"id":"start","type":"trigger"},{"id":"agent","type":"agent","prompt":"Research the asset thoroughly","output_mode":"json"},{"id":"transform","type":"transform","transform":"trim"}],"edges":[{"from":"start","to":"agent"},{"from":"agent","to":"transform"}]}`,
			valid: true,
		},
		{
			name:  "unknown output format",
			graph: `{"nodes":[{"id":"start","type":"trigger"},{"id":"agent","type":"agent","prompt":"Research the asset thoroughly","output_mode":"shell"}],"edges":[{"from":"start","to":"agent"}]}`,
			valid: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := storage.AgentFlow{Name: "Agent output", Graph: json.RawMessage(tc.graph)}
			_, _, err := validateFlow(f)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
