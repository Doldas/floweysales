package server

import "testing"

func TestFlowJSONContractValidation(t *testing.T) {
	contract := `{"type":"object","required":["symbol","score"],"properties":{"symbol":{"type":"string"},"score":{"type":"number"},"tags":{"type":"array"}}}`
	for _, tc := range []struct {
		name, output string
		valid        bool
	}{
		{"valid", `{"symbol":"NVDA","score":85,"tags":["growth"]}`, true},
		{"missing", `{"symbol":"NVDA"}`, false},
		{"type mismatch", `{"symbol":"NVDA","score":"high"}`, false},
		{"scalar", `"NVDA"`, false},
		{"invalid json", `{"symbol":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := enforceFlowJSONContract(tc.output, contract)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v; error=%v", tc.valid, err)
			}
		})
	}
}
func TestFlowJSONContractSafetyLimits(t *testing.T) {
	for _, raw := range []string{
		`{"type":"array"}`,
		`{"type":"object","required":["missing"]}`,
		`{"type":"object","properties":{"field":{"type":"any"}}}`,
		`{"type":"object","properties":{"bad-name":{"type":"string"}}}`,
	} {
		if _, err := validateFlowJSONContract(raw); err == nil {
			t.Fatalf("accepted invalid contract: %s", raw)
		}
	}
}
