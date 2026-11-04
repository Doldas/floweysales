// Package provenance describes where a displayed value came from.
//
// A Fact travels beside the data it describes and never inside it, so JSON
// contracts on the data stay valid. Unknown origins are explicit: a Fact is
// only "known" when it carries something a reader can follow.
package provenance

import (
	"errors"
	"time"
)

// Kind says how a value came to exist.
const (
	Observed    = "observed"     // read from a provider or document
	Calculated  = "calculated"   // computed from other facts by a stated method
	Assumed     = "assumed"      // chosen by a person or a default
	Estimated   = "estimated"    // approximated from partial data
	AIGenerated = "ai_generated" // written by a model; never counts as a source
)

// Status says how much of the origin is known.
const (
	Known   = "known"
	Partial = "partial"
	Unknown = "unknown"
)

// Method describes how a calculated value was derived.
type Method struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Formula string `json:"formula,omitempty"`
}

// Fact is the origin of one value.
type Fact struct {
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Provider    string     `json:"provider,omitempty"`
	Official    *bool      `json:"official,omitempty"` // nil when unknown
	URL         string     `json:"url,omitempty"`
	DocumentID  string     `json:"document_id,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	AsOf        *time.Time `json:"as_of,omitempty"`
	RetrievedAt *time.Time `json:"retrieved_at,omitempty"`
	Method      *Method    `json:"method,omitempty"`
	Inputs      []string   `json:"inputs,omitempty"` // references to upstream facts
	RunID       string     `json:"run_id,omitempty"`
	Note        string     `json:"note,omitempty"`
}

var validKinds = map[string]bool{Observed: true, Calculated: true, Assumed: true, Estimated: true, AIGenerated: true}

// Validate reports whether the Fact is consistent: a known Fact must name an origin a reader can follow.
func (f Fact) Validate() error {
	if !validKinds[f.Kind] {
		return errors.New("provenance: unknown kind " + f.Kind)
	}
	switch f.Status {
	case Unknown:
		return nil
	case Known, Partial:
	default:
		return errors.New("provenance: unknown status " + f.Status)
	}
	switch f.Kind {
	case Observed:
		if f.Provider == "" && f.URL == "" && f.DocumentID == "" {
			return errors.New("provenance: an observed fact needs a provider, URL or document")
		}
	case Calculated:
		if f.Method == nil || f.Method.ID == "" {
			return errors.New("provenance: a calculated fact needs a method")
		}
	}
	return nil
}

// Unspecified is the origin of a value nothing recorded a source for.
func Unspecified(note string) Fact {
	return Fact{Kind: Observed, Status: Unknown, Note: note}
}

// FromSource builds the Fact for a value read from a provider or document.
// It is "known" with a URL or document, "partial" with only a provider name, and "unknown" with neither.
func FromSource(provider, url, documentID string, published, retrieved time.Time) Fact {
	f := Fact{Kind: Observed, Provider: provider, URL: url, DocumentID: documentID}
	switch {
	case url != "" || documentID != "":
		f.Status = Known
	case provider != "":
		f.Status = Partial
	default:
		f.Status = Unknown
	}
	if !published.IsZero() {
		t := published.UTC()
		f.PublishedAt = &t
	}
	if !retrieved.IsZero() {
		t := retrieved.UTC()
		f.RetrievedAt = &t
	}
	return f
}

// Calculation builds the Fact for a value computed from other facts.
func Calculation(method Method, inputs ...string) (Fact, error) {
	f := Fact{Kind: Calculated, Status: Known, Method: &method, Inputs: inputs}
	return f, f.Validate()
}
