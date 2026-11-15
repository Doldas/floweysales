package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/instruments"
	"floweysales/internal/market"
	"floweysales/internal/provenance"
)

// Instrument intelligence (#441 phase 2): provider profiles, the platform
// taxonomy, each user's own classification and how a collection's members
// relate to each other. A profile is read only from the provider the
// instrument belongs to: a symbol means something only to its own provider,
// so another provider's answer could describe a different instrument. That is
// why the "profile" routing domain allows no fallback. Profiles are provider
// facts shared by every user and read with the system plugin instance;
// classifications are private to the user who made them.

// Statuses of an instrument's profile as the API reports them.
const (
	profileOK          = instruments.StatusOK
	profileNotFound    = instruments.StatusNotFound
	profileNoSource    = "no_source"   // the instrument's provider offers no profiles
	profileMissing     = "missing"     // offered, but not fetched yet
	profileUnavailable = "unavailable" // the provider failed and nothing is cached
)

// Bounds on asking providers.
const (
	profileRetryAfter   = time.Minute      // per instrument, between two requests to its provider
	profileFetchTimeout = 15 * time.Second // per request
	profileRefreshBatch = 20               // instruments per collection refresh call
	profileRefreshPar   = 4                // requests in flight per refresh call
)

func (e *Engine) instrumentStoreFor() (instruments.Store, bool) {
	if e.instrumentStore != nil {
		return e.instrumentStore, true
	}
	if e.db != nil {
		return e.db, true
	}
	return nil, false
}

func (e *Engine) profileSource() instruments.ProfileSource {
	if s, ok := e.providers.(instruments.ProfileSource); ok {
		return s
	}
	return nil
}

// profileProviders is the set of providers that offer profiles right now.
func (e *Engine) profileProviders() map[string]bool {
	out := map[string]bool{}
	if s := e.profileSource(); s != nil {
		for _, p := range s.ProfileProviders() {
			out[p.ID()] = true
		}
	}
	return out
}

// profileAttempts remembers when each instrument's provider was last asked,
// so repeated views and refreshes do not hammer a provider.
type profileAttempts struct {
	mu   sync.Mutex
	last map[instruments.Key]time.Time
}

// claim reports whether the provider may be asked about k now, and records it.
func (a *profileAttempts) claim(k instruments.Key, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.last == nil {
		a.last = map[instruments.Key]time.Time{}
	}
	if t, ok := a.last[k]; ok && now.Sub(t) < profileRetryAfter {
		return false
	}
	if len(a.last) > 10000 {
		for key, t := range a.last {
			if now.Sub(t) >= profileRetryAfter {
				delete(a.last, key)
			}
		}
	}
	a.last[k] = now
	return true
}

// profileResult is what the API says about one instrument's profile.
type profileResult struct {
	Key         instruments.Key      `json:"key"`
	Status      string               `json:"status"`
	Profile     *instruments.Profile `json:"profile,omitempty"`
	RetrievedAt *time.Time           `json:"retrieved_at,omitempty"`
	ExpiresAt   *time.Time           `json:"expires_at,omitempty"`
	Stale       bool                 `json:"stale"`
	Error       string               `json:"error,omitempty"`
	Provenance  *provenance.Fact     `json:"provenance,omitempty"`
}

func resultOf(k instruments.Key, c *instruments.CachedProfile, now time.Time) profileResult {
	r := profileResult{Key: k}
	if c == nil {
		return r
	}
	r.Status, r.Profile = c.Status, c.Profile
	retrieved, expires := c.RetrievedAt, c.ExpiresAt
	r.RetrievedAt, r.ExpiresAt = &retrieved, &expires
	r.Stale = !c.Fresh(now)
	f := profileProvenance(*c)
	r.Provenance = &f
	return r
}

// profileProvenance attributes a cached answer to the provider that gave it:
// known with the provider's own page for the instrument, partial without one.
func profileProvenance(c instruments.CachedProfile) provenance.Fact {
	url := ""
	if c.Profile != nil {
		url = c.Profile.SourceURL
	}
	f := provenance.FromSource(c.Provider, url, "", time.Time{}, c.RetrievedAt)
	if c.Profile != nil {
		if t, err := time.Parse(time.RFC3339, c.Profile.AsOf); err == nil {
			t = t.UTC()
			f.AsOf = &t
		}
	}
	f.Note = "Profile as the provider returned it, cleaned of malformed values; FloweySales does not verify it."
	if c.Status == instruments.StatusNotFound {
		f.Note = "The provider said it has no profile for this symbol."
	}
	return f
}

// loadInstrumentProfile returns the cached profile of k and, when it is missing or
// expired (or force is set), asks the instrument's own provider. A failed
// request keeps whatever is cached; errors are never cached.
func (e *Engine) loadInstrumentProfile(ctx context.Context, store instruments.Store, k instruments.Key, force bool) (profileResult, error) {
	now := time.Now().UTC()
	if !e.profileProviders()[k.Provider] {
		return profileResult{Key: k, Status: profileNoSource}, nil
	}
	cached, err := store.GetProfiles(ctx, []instruments.Key{k})
	if err != nil {
		return profileResult{}, err
	}
	var current *instruments.CachedProfile
	if c, ok := cached[k]; ok {
		current = &c
	}
	r := resultOf(k, current, now)
	if current != nil && current.Fresh(now) && !force {
		return r, nil
	}
	if !e.profileAttempts.claim(k, now) {
		if current == nil {
			r.Status, r.Error = profileUnavailable, "The provider was asked a moment ago; try again in a minute."
		}
		return r, nil
	}
	fetchCtx, cancel := context.WithTimeout(ctx, profileFetchTimeout)
	defer cancel()
	p, found, err := e.profileSource().Profile(fetchCtx, market.Provider(k.Provider), k.Symbol)
	if err != nil {
		log.Printf("instrument profile %s: %v", k, err)
		if current == nil {
			r.Status = profileUnavailable
		}
		r.Error = "The provider could not be reached; nothing new was stored."
		return r, nil
	}
	entry := instruments.CachedProfile{Key: k, Status: instruments.StatusNotFound, RetrievedAt: now, ExpiresAt: now.Add(instruments.NotFoundTTL)}
	if found {
		entry.Status, entry.Profile, entry.ExpiresAt = instruments.StatusOK, &p, now.Add(instruments.ProfileTTL)
	}
	if err := store.PutProfile(ctx, entry); err != nil {
		return profileResult{}, err
	}
	return resultOf(k, &entry, now), nil
}

// instrumentRequest starts every instrument handler: the caller and the store.
func (e *Engine) instrumentRequest(w http.ResponseWriter, r *http.Request) (int64, instruments.Store, bool) {
	u, ok := e.planUser(w, r)
	if !ok {
		return 0, nil, false
	}
	store, ok := e.instrumentStoreFor()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Instrument profiles need the database"})
		return 0, nil, false
	}
	return u.ID, store, true
}

func writeInstrumentError(w http.ResponseWriter, err error) {
	if errors.Is(err, instruments.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	log.Printf("instruments: %v", err)
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Instrument profiles are unavailable right now"})
}

func keyFromQuery(r *http.Request) (instruments.Key, error) {
	return instruments.NormalizeKey(r.URL.Query().Get("provider"), r.URL.Query().Get("symbol"))
}

// overrideOf returns the user's override of k, or nil.
func overrideOf(ctx context.Context, store instruments.Store, userID int64, k instruments.Key) (*instruments.Override, error) {
	list, err := store.GetOverrides(ctx, userID, []instruments.Key{k})
	if err != nil {
		return nil, err
	}
	if o, ok := list[k]; ok {
		return &o, nil
	}
	return nil, nil
}

// classificationDraft is the body of PUT /api/instruments/classification.
type classificationDraft struct {
	Provider   string   `json:"provider"`
	Symbol     string   `json:"symbol"`
	AssetClass string   `json:"asset_class"`
	Sector     string   `json:"sector"`
	Industry   string   `json:"industry"`
	Country    string   `json:"country"`
	Themes     []string `json:"themes"`
}

// memberIntelligence is one member in GET /api/collections/{id}/intelligence.
type memberIntelligence struct {
	MemberID       int64                      `json:"member_id"`
	Key            instruments.Key            `json:"key"`
	Name           string                     `json:"name,omitempty"`
	ProfileStatus  string                     `json:"profile_status"`
	Stale          bool                       `json:"stale"`
	RetrievedAt    *time.Time                 `json:"retrieved_at,omitempty"`
	Provenance     *provenance.Fact           `json:"provenance,omitempty"`
	Classification instruments.Classification `json:"classification"`
	Overridden     bool                       `json:"overridden"`
	Weight         *float64                   `json:"weight,omitempty"`
}

// compositionRow counts members (and sums weights) for one taxonomy value.
// An empty id collects the members nothing classifies.
type compositionRow struct {
	ID     string   `json:"id"`
	Count  int      `json:"count"`
	Weight *float64 `json:"weight,omitempty"`
}

func composition(members []memberIntelligence, value func(memberIntelligence) []string) []compositionRow {
	rows := map[string]*compositionRow{}
	for _, m := range members {
		ids := value(m)
		if len(ids) == 0 {
			ids = []string{""}
		}
		for _, id := range ids {
			row := rows[id]
			if row == nil {
				row = &compositionRow{ID: id}
				rows[id] = row
			}
			row.Count++
			if m.Weight != nil {
				sum := *m.Weight
				if row.Weight != nil {
					sum += *row.Weight
				}
				row.Weight = &sum
			}
		}
	}
	out := make([]compositionRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(a, b int) bool {
		if (out[a].ID == "") != (out[b].ID == "") {
			return out[b].ID == ""
		}
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// collectionMembers loads all members of one of the caller's collections.
func (e *Engine) collectionMembers(w http.ResponseWriter, r *http.Request) (int64, collections.Collection, []collections.Member, instruments.Store, bool) {
	u, svc, ok := e.collectionRequest(w, r)
	if !ok {
		return 0, collections.Collection{}, nil, nil, false
	}
	store, ok := e.instrumentStoreFor()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Instrument profiles need the database"})
		return 0, collections.Collection{}, nil, nil, false
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return 0, collections.Collection{}, nil, nil, false
	}
	c, err := svc.Get(r.Context(), u.ID, id)
	if err != nil {
		writeCollectionError(w, err)
		return 0, collections.Collection{}, nil, nil, false
	}
	members, err := svc.Store.AllMembers(r.Context(), u.ID, id)
	if err != nil {
		writeCollectionError(w, err)
		return 0, collections.Collection{}, nil, nil, false
	}
	return u.ID, c, members, store, true
}

func memberKeys(members []collections.Member) []instruments.Key {
	seen := map[instruments.Key]bool{}
	var keys []instruments.Key
	for _, m := range members {
		k := instruments.KeyOf(m.Instrument)
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

func (e *Engine) registerInstrumentRoutes() {
	e.mux.HandleFunc("GET /api/instruments/taxonomy", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := e.planUser(w, r); !ok {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"asset_classes": instruments.AssetClasses, "sectors": instruments.Sectors, "regions": instruments.Regions,
			"limits": map[string]int{"themes": instruments.MaxThemes, "theme_length": instruments.MaxThemeLen, "industry_length": instruments.MaxIndustry}})
	})

	e.mux.HandleFunc("GET /api/instruments/profile", func(w http.ResponseWriter, r *http.Request) {
		userID, store, ok := e.instrumentRequest(w, r)
		if !ok {
			return
		}
		k, err := keyFromQuery(r)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		res, err := e.loadInstrumentProfile(r.Context(), store, k, r.URL.Query().Get("refresh") == "1")
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		o, err := overrideOf(r.Context(), store, userID, k)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": res,
			"classification": instruments.Classify(collections.Instrument{Provider: k.Provider, Symbol: k.Symbol}, res.Profile, o), "override": o})
	})

	e.mux.HandleFunc("PUT /api/instruments/classification", func(w http.ResponseWriter, r *http.Request) {
		userID, store, ok := e.instrumentRequest(w, r)
		if !ok {
			return
		}
		var d classificationDraft
		if !decodeCollectionBody(w, r, 8192, &d) {
			return
		}
		o, err := instruments.NormalizeOverride(instruments.Override{Key: instruments.Key{Provider: d.Provider, Symbol: d.Symbol},
			AssetClass: d.AssetClass, Sector: d.Sector, Industry: d.Industry, Country: d.Country, Themes: d.Themes})
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		if err := store.PutOverride(r.Context(), userID, o); err != nil {
			writeInstrumentError(w, err)
			return
		}
		saved, err := overrideOf(r.Context(), store, userID, o.Key)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"override": saved})
	})

	e.mux.HandleFunc("DELETE /api/instruments/classification", func(w http.ResponseWriter, r *http.Request) {
		userID, store, ok := e.instrumentRequest(w, r)
		if !ok {
			return
		}
		k, err := keyFromQuery(r)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		if err := store.DeleteOverride(r.Context(), userID, k); err != nil {
			writeInstrumentError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// The cache only: opening a collection never asks a provider.
	e.mux.HandleFunc("GET /api/collections/{id}/intelligence", func(w http.ResponseWriter, r *http.Request) {
		userID, c, members, store, ok := e.collectionMembers(w, r)
		if !ok {
			return
		}
		out, _, err := e.collectionIntelligence(r.Context(), userID, c, members, store)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	// Fetches the profiles that are missing or expired, a bounded batch at a time.
	e.mux.HandleFunc("POST /api/collections/{id}/profiles/refresh", func(w http.ResponseWriter, r *http.Request) {
		_, _, members, store, ok := e.collectionMembers(w, r)
		if !ok {
			return
		}
		keys := memberKeys(members)
		cached, err := store.GetProfiles(r.Context(), keys)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		offered := e.profileProviders()
		now := time.Now().UTC()
		counts := map[string]int{"fetched": 0, "not_found": 0, "failed": 0, "no_source": 0, "fresh": 0, "remaining": 0}
		var due []instruments.Key
		for _, k := range keys {
			switch c, ok := cached[k]; {
			case !offered[k.Provider]:
				counts["no_source"]++
			case ok && c.Fresh(now):
				counts["fresh"]++
			default:
				due = append(due, k)
			}
		}
		if len(due) > profileRefreshBatch {
			counts["remaining"] = len(due) - profileRefreshBatch
			due = due[:profileRefreshBatch]
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		slots := make(chan struct{}, profileRefreshPar)
		var storeErr error
		for _, k := range due {
			wg.Add(1)
			slots <- struct{}{}
			go func(k instruments.Key) {
				defer wg.Done()
				defer func() { <-slots }()
				res, err := e.loadInstrumentProfile(r.Context(), store, k, false)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err != nil:
					storeErr = err
				case res.Error != "" || res.Status == profileUnavailable:
					counts["failed"]++
				case res.Status == profileOK:
					counts["fetched"]++
				case res.Status == profileNotFound:
					counts["not_found"]++
				}
			}(k)
		}
		wg.Wait()
		if storeErr != nil {
			writeInstrumentError(w, storeErr)
			return
		}
		writeJSON(w, http.StatusOK, counts)
	})

	e.mux.HandleFunc("DELETE /api/admin/instruments/profiles", func(w http.ResponseWriter, r *http.Request) {
		store, ok := e.instrumentStoreFor()
		if !ok {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Instrument profiles need the database"})
			return
		}
		provider := ""
		if raw := strings.TrimSpace(r.URL.Query().Get("provider")); raw != "" {
			p := market.ParseProvider(raw)
			if !p.Valid() {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid provider"})
				return
			}
			provider = p.ID()
		}
		n, err := store.DeleteProfiles(r.Context(), provider)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
	})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// isInstrumentRoute matches the instrument routes every signed-in user may
// call. Profiles are shared provider facts; classifications are the caller's own.
func isInstrumentRoute(method, path string) bool {
	switch path {
	case "/api/instruments/profile", "/api/instruments/taxonomy":
		return method == http.MethodGet
	case "/api/instruments/classification":
		return method == http.MethodPut || method == http.MethodDelete
	}
	return false
}

// collectionIntelligence reads the cached profiles and the user's
// classifications of a collection's members. It never asks a provider.
func (e *Engine) collectionIntelligence(ctx context.Context, userID int64, c collections.Collection, members []collections.Member, store instruments.Store) (map[string]any, []memberIntelligence, error) {
	keys := memberKeys(members)
	cached, err := store.GetProfiles(ctx, keys)
	if err != nil {
		return nil, nil, err
	}
	overrides, err := store.GetOverrides(ctx, userID, keys)
	if err != nil {
		return nil, nil, err
	}
	offered := e.profileProviders()
	now := time.Now().UTC()
	profiles := map[instruments.Key]*instruments.Profile{}
	coverage := map[string]int{profileOK: 0, profileNotFound: 0, profileMissing: 0, profileNoSource: 0, "stale": 0, "overridden": 0}
	out := make([]memberIntelligence, 0, len(members))
	for _, m := range members {
		k := instruments.KeyOf(m.Instrument)
		mi := memberIntelligence{MemberID: m.ID, Key: k, Name: m.Instrument.Name, Weight: m.Weight}
		var p *instruments.Profile
		if entry, ok := cached[k]; ok {
			// A cached profile stays useful after its provider is removed or it expires.
			mi.ProfileStatus, mi.Stale, p = entry.Status, !entry.Fresh(now), entry.Profile
			retrieved := entry.RetrievedAt
			mi.RetrievedAt = &retrieved
			f := profileProvenance(entry)
			mi.Provenance = &f
			profiles[k] = p
		} else if offered[k.Provider] {
			mi.ProfileStatus = profileMissing
		} else {
			mi.ProfileStatus = profileNoSource
		}
		if mi.Name == "" && p != nil {
			mi.Name = p.Name
		}
		var o *instruments.Override
		if v, ok := overrides[k]; ok {
			o, mi.Overridden = &v, true
			coverage["overridden"]++
		}
		mi.Classification = instruments.Classify(m.Instrument, p, o)
		coverage[mi.ProfileStatus]++
		if mi.Stale {
			coverage["stale"]++
		}
		out = append(out, mi)
	}
	one := func(f func(instruments.Classification) instruments.Fact) func(memberIntelligence) []string {
		return func(m memberIntelligence) []string {
			if v := f(m.Classification).Value; v != "" {
				return []string{v}
			}
			return nil
		}
	}
	return map[string]any{
		"collection_id": c.ID, "kind": c.Kind, "members": out, "coverage": coverage,
		"links": instruments.Links(members, profiles),
		"composition": map[string]any{
			"asset_class": composition(out, one(func(c instruments.Classification) instruments.Fact { return c.AssetClass })),
			"sector":      composition(out, one(func(c instruments.Classification) instruments.Fact { return c.Sector })),
			"region":      composition(out, one(func(c instruments.Classification) instruments.Fact { return c.Region })),
			"theme":       composition(out, func(m memberIntelligence) []string { return m.Classification.Themes }),
		},
		"profile_providers": sortedKeys(offered),
	}, out, nil
}
