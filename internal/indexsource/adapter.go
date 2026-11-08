package indexsource

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Adapter kinds.
const (
	KindFile = "file" // the user uploads the list
	KindAPI  = "api"  // the list is fetched with the user's own credentials
)

// AdapterInfo describes a way to obtain a constituent list. It is shown to
// users and never contains data or credentials.
type AdapterInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	NeedsKey bool   `json:"needs_key"`
	// Indexes are the registry ids it can serve; empty means any index.
	Indexes []string `json:"indexes,omitempty"`
	// Documented is false while the response shape has not been checked
	// against a live response, so lists from it are marked unverified.
	Documented   bool   `json:"documented"`
	Restrictions string `json:"restrictions"`
}

// Fetcher is an adapter that retrieves a list. Implementations receive only
// the user's own credentials, return the full list of problems when they
// refuse, and must never invent a row.
type Fetcher interface {
	Info() AdapterInfo
	Fetch(ctx context.Context, spec Spec, meta Meta, now time.Time) (Snapshot, []string, error)
}

// LicenceNotice is the restriction every adapter carries: a key or a file
// proves access, not the right to show or keep derived data.
const LicenceNotice = "Having an API key or a file does not by itself give you the right to display, store or share index constituent data. Your agreement with the provider decides that, and FloweySales does not assume any plan allows it. The list is stored for your account only and is never shared or shipped with the app."

var (
	regMu    sync.RWMutex
	registry = map[string]AdapterInfo{}
	fetchers = map[string]Fetcher{}
)

func init() {
	RegisterInfo(AdapterInfo{ID: ProviderCSV, Name: "CSV import", Kind: KindFile, Documented: true,
		Restrictions: LicenceNotice + " You attest to the source and the terms when you import."})
	ids := make([]string, 0, len(FMPIndexes))
	for id := range FMPIndexes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	RegisterInfo(AdapterInfo{ID: ProviderFMP, Name: "Financial Modeling Prep", Kind: KindAPI, NeedsKey: true, Indexes: ids, Documented: true,
		Restrictions: LicenceNotice + " Check the terms of your plan; some plans limit what may be shown or kept. Endpoints for some indices are marked unverified."})
}

// RegisterInfo adds or replaces an adapter description.
func RegisterInfo(i AdapterInfo) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[i.ID] = i
}

// Register adds a fetching adapter. A later registration under the same id
// replaces the earlier one.
func Register(f Fetcher) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[f.Info().ID] = f.Info()
	fetchers[f.Info().ID] = f
}

// Lookup returns the fetcher registered under id.
func Lookup(id string) (Fetcher, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := fetchers[id]
	return f, ok
}

// Adapters lists every adapter that can serve the index (all when indexID is
// empty), ordered by id.
func Adapters(indexID string) []AdapterInfo {
	regMu.RLock()
	defer regMu.RUnlock()
	var out []AdapterInfo
	for _, i := range registry {
		if indexID != "" && len(i.Indexes) > 0 {
			match := false
			for _, id := range i.Indexes {
				if id == indexID {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

func adapterInfo(id string) (AdapterInfo, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	i, ok := registry[id]
	return i, ok
}
