package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Domain providers offer something other than market data, for example tax
// calculations. A provider has a Type; the type fixes its capabilities; each
// capability is an operation with a declared input that the app and the AI
// agent call generically. This mirrors pluginsdk/domain.go.

// Provider types. An empty type means TypeMarket.
const (
	TypeMarket = "market"
	TypeTax    = "tax"
)

// Capabilities of the tax type.
const (
	CapTaxReport   = "tax_report"
	CapTaxHoldings = "tax_holdings"
	CapTaxEstimate = "tax_estimate"
)

var domainTypes = map[string][]string{
	TypeTax: {CapTaxReport, CapTaxHoldings, CapTaxEstimate},
}

// Limits of domain operations.
const (
	MaxDomainOperations  = 16
	MaxDomainFields      = 24
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

// Sources the backend can fill an input from when the caller leaves it empty.
const (
	// SourceUserTransactions fills a string input with the caller's saved
	// transaction exports, each file preceded by a DomainFileMarker line.
	SourceUserTransactions = "user_transactions"
	// DomainFileMarker starts each file in a multi-file input: "=== file: NAME ===".
	DomainFileMarker = "=== file: "
)

// DomainField is one input of an operation.
type DomainField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	// Source, when set, lets the backend fill the input from the caller's own
	// stored data if the caller leaves it empty (see SourceUserTransactions).
	Source string `json:"source,omitempty"`
}

// DomainOperation describes one capability of a domain provider.
type DomainOperation struct {
	Capability  string        `json:"capability"`
	Description string        `json:"description"`
	Inputs      []DomainField `json:"inputs,omitempty"`
}

// ErrDomainInput marks a call the backend rejected before reaching the plugin.
var ErrDomainInput = errors.New("invalid input")

// ErrNoSuchDomain is returned for an unknown provider or operation.
var ErrNoSuchDomain = errors.New("no such domain provider or operation")

var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// IsDomain reports whether the provider is a domain provider.
func (p ProviderInfo) IsDomain() bool {
	_, ok := domainTypes[p.Type]
	return ok
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
			if f.Source != "" && (f.Source != SourceUserTransactions || f.Type != FieldString) {
				return fmt.Errorf("operation %s input %q: unknown source %q or not a string input", op.Capability, f.Name, f.Source)
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

// DomainProvider is an installed domain provider as the app and the agent see it.
type DomainProvider struct {
	ProviderID   string            `json:"provider_id"`
	Label        string            `json:"label"`
	Type         string            `json:"type"`
	PluginID     string            `json:"plugin_id"`
	Version      string            `json:"version"`
	Ready        bool              `json:"ready"`
	Missing      []string          `json:"missing,omitempty"`
	Capabilities []string          `json:"capabilities"`
	Operations   []DomainOperation `json:"operations"`
	Notes        string            `json:"notes,omitempty"`
}

// DomainProviders lists installed domain providers, optionally of one type.
func (m *Manager) DomainProviders(providerType string) []DomainProvider {
	out := []DomainProvider{}
	for _, p := range m.Installed() {
		if !p.Manifest.ProvidesDomain() || (providerType != "" && p.Manifest.Provider.Type != providerType) {
			continue
		}
		info := p.Manifest.Provider
		_, missing := m.env(&p.Manifest)
		out = append(out, DomainProvider{ProviderID: info.ID, Label: info.Label, Type: info.Type, PluginID: p.Manifest.ID, Version: p.Manifest.Version,
			Ready: len(missing) == 0, Missing: missing, Capabilities: append([]string{}, info.Capabilities...), Operations: info.Operations, Notes: info.Notes})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out
}

// checkDomainInput validates input against the operation's declared fields:
// unknown names, wrong types, missing required fields and enum misses are
// rejected, so a plugin only ever sees what it declared.
func checkDomainInput(op DomainOperation, input map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaxDomainInputBytes {
		return nil, fmt.Errorf("%w: input is larger than %d MB", ErrDomainInput, MaxDomainInputBytes>>20)
	}
	known := map[string]bool{}
	clean := map[string]any{}
	for _, f := range op.Inputs {
		known[f.Name] = true
		v, present := input[f.Name]
		if !present || v == nil || v == "" {
			if f.Required {
				return nil, fmt.Errorf("%w: %s is required", ErrDomainInput, f.Name)
			}
			continue
		}
		switch f.Type {
		case FieldString:
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be text", ErrDomainInput, f.Name)
			}
			if len(f.Enum) > 0 {
				found := false
				for _, e := range f.Enum {
					found = found || e == s
				}
				if !found {
					return nil, fmt.Errorf("%w: %s must be one of %s", ErrDomainInput, f.Name, strings.Join(f.Enum, ", "))
				}
			}
			clean[f.Name] = s
		case FieldNumber:
			n, ok := v.(float64)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be a number", ErrDomainInput, f.Name)
			}
			clean[f.Name] = n
		case FieldInteger:
			n, ok := v.(float64)
			if !ok || n != float64(int64(n)) {
				return nil, fmt.Errorf("%w: %s must be a whole number", ErrDomainInput, f.Name)
			}
			clean[f.Name] = int64(n)
		case FieldBoolean:
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be true or false", ErrDomainInput, f.Name)
			}
			clean[f.Name] = b
		}
	}
	for name := range input {
		if !known[name] {
			return nil, fmt.Errorf("%w: %s is not an input of %s", ErrDomainInput, name, op.Capability)
		}
	}
	return clean, nil
}

// CallDomain runs one operation of an installed domain provider. The input is
// checked against the declared fields; nothing is stored or logged here.
func (m *Manager) CallDomain(ctx context.Context, providerID, operation string, input map[string]any) (json.RawMessage, error) {
	m.mu.Lock()
	installed, ok := m.byProviderLocked(strings.ToUpper(strings.TrimSpace(providerID)))
	m.mu.Unlock()
	if !ok || !installed.Manifest.ProvidesDomain() {
		return nil, fmt.Errorf("%w: provider %q is not installed", ErrNoSuchDomain, providerID)
	}
	op, ok := installed.Manifest.Provider.Operation(operation)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no operation %q", ErrNoSuchDomain, installed.Manifest.Provider.ID, operation)
	}
	if _, missing := m.env(&installed.Manifest); len(missing) > 0 {
		return nil, &CallError{Code: CodeNotConfigured, Message: strings.Join(missing, ", ") + " must be configured on the backend"}
	}
	clean, err := checkDomainInput(op, input)
	if err != nil {
		return nil, err
	}
	proc, err := m.process(installed)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.CallTimeout)
	defer cancel()
	var out json.RawMessage
	if err := proc.call(ctx, "domain", map[string]any{"operation": operation, "input": clean}, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 || len(out) > MaxDomainResultBytes || !json.Valid(out) {
		return nil, &CallError{Code: CodeInternal, Message: fmt.Sprintf("plugin returned no valid JSON result of at most %d KB", MaxDomainResultBytes>>10)}
	}
	return out, nil
}
