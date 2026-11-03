package pluginsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Domain providers are providers of something other than market data, for
// example tax calculations. A provider has a Type, the type fixes which
// capabilities exist, and each capability is one operation with a declared
// input, so the app and the AI agent can discover and call it generically.
// The plugin answers all of them through the one "domain" method.

// Provider types. An empty type means TypeMarket.
const (
	TypeMarket = "market"
	TypeTax    = "tax"
)

// Capabilities of the tax type. Each is an operation of the same name.
const (
	CapTaxReport   = "tax_report"   // sales of the income year with cost basis, totals and a tax estimate
	CapTaxHoldings = "tax_holdings" // what is held at a date, with average cost
	CapTaxEstimate = "tax_estimate" // tax on gains and losses the caller already knows
)

// domainTypes lists the capabilities each domain type defines.
var domainTypes = map[string][]string{
	TypeTax: {CapTaxReport, CapTaxHoldings, CapTaxEstimate},
}

// DomainTypes returns the known domain provider types.
func DomainTypes() []string { return []string{TypeTax} }

// IsDomain reports whether the provider is a domain provider.
func (p ProviderInfo) IsDomain() bool {
	_, ok := domainTypes[p.Type]
	return ok
}

// Limits of domain operations.
const (
	MaxDomainOperations = 16
	MaxDomainFields     = 24
	// MaxDomainInputBytes bounds the JSON input of one call; MaxDomainResultBytes the JSON result.
	MaxDomainInputBytes  = 4 << 20
	MaxDomainResultBytes = 1 << 20
)

// Field types of an operation's input.
const (
	FieldString  = "string"
	FieldNumber  = "number"
	FieldInteger = "integer"
	FieldBoolean = "boolean"
)

// DomainField is one input of an operation.
type DomainField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// DomainOperation describes one capability of a domain provider.
type DomainOperation struct {
	// Capability is the capability this operation implements, and its name.
	Capability  string        `json:"capability"`
	Description string        `json:"description"`
	Inputs      []DomainField `json:"inputs,omitempty"`
}

// DomainParams is the "domain" request: an operation and its JSON input.
type DomainParams struct {
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input,omitempty"`
}

var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// validateDomain checks a domain provider's declaration.
func (p ProviderInfo) validateDomain() error {
	allowed := map[string]bool{}
	for _, c := range domainTypes[p.Type] {
		allowed[c] = true
	}
	if len(p.Research) > 0 {
		return errors.New("research operations belong to market-data providers")
	}
	if len(p.Capabilities) == 0 {
		return errors.New("provider declares no capabilities")
	}
	declared := map[string]bool{}
	for _, c := range p.Capabilities {
		if !allowed[c] {
			return fmt.Errorf("capability %q does not exist for provider type %q", c, p.Type)
		}
		if declared[c] {
			return fmt.Errorf("duplicate capability %q", c)
		}
		declared[c] = true
	}
	if len(p.Operations) > MaxDomainOperations {
		return fmt.Errorf("a provider declares at most %d operations", MaxDomainOperations)
	}
	described := map[string]bool{}
	for _, op := range p.Operations {
		if !declared[op.Capability] || described[op.Capability] {
			return fmt.Errorf("operation %q is not a declared capability or is described twice", op.Capability)
		}
		described[op.Capability] = true
		if op.Description == "" || len(op.Description) > 600 {
			return fmt.Errorf("operation %s needs a description of up to 600 characters", op.Capability)
		}
		if len(op.Inputs) > MaxDomainFields {
			return fmt.Errorf("operation %s: at most %d inputs", op.Capability, MaxDomainFields)
		}
		names := map[string]bool{}
		for _, f := range op.Inputs {
			switch f.Type {
			case FieldString, FieldNumber, FieldInteger, FieldBoolean:
			default:
				return fmt.Errorf("operation %s input %q: unknown type %q", op.Capability, f.Name, f.Type)
			}
			if !fieldNamePattern.MatchString(f.Name) || names[f.Name] {
				return fmt.Errorf("operation %s: invalid or duplicate input name %q", op.Capability, f.Name)
			}
			if len(f.Enum) > 0 && f.Type != FieldString {
				return fmt.Errorf("operation %s input %q: enum only applies to strings", op.Capability, f.Name)
			}
			if len(f.Description) > 400 {
				return fmt.Errorf("operation %s input %q: description is too long", op.Capability, f.Name)
			}
			names[f.Name] = true
		}
	}
	for c := range declared {
		if !described[c] {
			return fmt.Errorf("capability %q has no operation description", c)
		}
	}
	return nil
}

// Operation finds a declared operation.
func (p ProviderInfo) Operation(capability string) (DomainOperation, bool) {
	for _, op := range p.Operations {
		if op.Capability == capability {
			return op, true
		}
	}
	return DomainOperation{}, false
}

// runDomain answers a "domain" request for an operation the manifest declares.
func (p Plugin) runDomain(ctx context.Context, raw json.RawMessage) (any, error) {
	if p.Domain == nil || !p.Manifest.Provider.IsDomain() {
		return nil, Errorf(CodeUnsupported, "method not supported by this plugin")
	}
	params, err := decode[DomainParams](raw)
	if err != nil {
		return nil, err
	}
	if _, ok := p.Manifest.Provider.Operation(params.Operation); !ok {
		return nil, Errorf(CodeInvalidArgument, "unknown operation %q", params.Operation)
	}
	return p.Domain(ctx, params)
}
