package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type domainStub struct {
	fakeDS
	gotProvider, gotOperation string
	gotInput                  map[string]any
}

func (d *domainStub) DomainProviders(t string) (any, error) {
	return map[string]any{"providers": []string{"SE_TAX"}, "type": t}, nil
}

func (d *domainStub) CallDomainProvider(_ context.Context, provider, operation string, input map[string]any) (any, error) {
	d.gotProvider, d.gotOperation, d.gotInput = provider, operation, input
	if operation == "boom" {
		return nil, errors.New("year is required")
	}
	return map[string]any{"ok": true}, nil
}

func TestDomainProviderToolsAreAdvertisedAndRouted(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range ToolDefs() {
		names[tool.Function.Name] = true
	}
	if !names["list_domain_providers"] || !names["call_domain_provider"] {
		t.Fatal("the domain provider tools must be in the catalogue")
	}
	stub := &domainStub{}
	out, err := Execute(context.Background(), stub, "list_domain_providers", `{"type":"tax"}`)
	if err != nil || !strings.Contains(out, "SE_TAX") || !strings.Contains(out, `"type":"tax"`) {
		t.Fatalf("%s %v", out, err)
	}
	out, err = Execute(context.Background(), stub, "call_domain_provider", `{"provider":"SE_TAX","operation":"tax_report","input":{"year":2025}}`)
	if err != nil || !strings.Contains(out, `"ok":true`) || stub.gotProvider != "SE_TAX" || stub.gotOperation != "tax_report" || stub.gotInput["year"] != float64(2025) {
		t.Fatalf("%s %v %+v", out, err, stub)
	}
	if _, err := Execute(context.Background(), stub, "call_domain_provider", `{"provider":"SE_TAX"}`); err == nil {
		t.Fatal("an operation is required")
	}
	if _, err := Execute(context.Background(), stub, "call_domain_provider", `{"provider":"SE_TAX","operation":"boom"}`); err == nil || !strings.Contains(err.Error(), "year is required") {
		t.Fatalf("a provider error must reach the agent: %v", err)
	}
	if _, err := Execute(context.Background(), fakeDS{}, "list_domain_providers", `{}`); !errors.Is(err, ErrDomainToolsUnavailable) {
		t.Fatalf("a data source without domain providers: %v", err)
	}
}
