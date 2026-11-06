package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/indexsource"
	"floweysales/internal/storage"
)

// User-supplied index constituents (#258). FloweySales ships no licensed
// constituent list. A user imports a CSV file or fetches a list with their own
// Financial Modeling Prep key; the index is then available for that user only
// and the data is stored per user, never in the repository. The role model
// holds: an administrator sets up the system, users choose their own data and
// keys.

// indexSourceStore keeps each user's constituent snapshots.
type indexSourceStore interface {
	LoadIndexSource(ctx context.Context, userID int64, indexID string) (string, error)
	SaveIndexSource(ctx context.Context, userID int64, indexID, data string) error
	DeleteIndexSource(ctx context.Context, userID int64, indexID string) error
}

// indexSecretStore keeps each user's sealed Financial Modeling Prep key. The
// signatures are those of the integration-secret table.
type indexSecretStore interface {
	SaveAPIIntegrationSecret(ctx context.Context, kind, provider string, ciphertext []byte) error
	GetAPIIntegrationSecret(ctx context.Context, kind string) (storage.APIIntegrationSecret, error)
	DeleteAPIIntegrationSecret(ctx context.Context, kind string) error
}

func (e *Engine) indexSourceStore() indexSourceStore {
	if e.indexSources != nil {
		return e.indexSources
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

func (e *Engine) indexSecretStore() indexSecretStore {
	if e.indexSecrets != nil {
		return e.indexSecrets
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

func (e *Engine) indexHTTPClient() indexsource.HTTPDoer {
	if e.indexHTTP != nil {
		return e.indexHTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func indexKeyKind(userID int64) string { return fmt.Sprintf("index_fmp:%d", userID) }

type indexUserKey struct{}

// withIndexUser records whose request this is, so code far from the handler
// (a backtest parsed from a saved query) can find that user's own data.
func withIndexUser(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, indexUserKey{}, id)
}

// indexUserID is the signed-in user, or 0 when authentication is off. ok is
// false when authentication is on and no user is attached (a flow or agent
// run): such callers see no user-supplied index.
func (e *Engine) indexUserID(ctx context.Context) (int64, bool) {
	if id, ok := ctx.Value(indexUserKey{}).(int64); ok {
		return id, true
	}
	return 0, e.authService == nil
}

// userSnapshot loads the caller's snapshot for an index; nil when none.
func (e *Engine) userSnapshot(ctx context.Context, userID int64, indexID string) (*indexsource.Snapshot, error) {
	store := e.indexSourceStore()
	if store == nil {
		return nil, nil
	}
	raw, err := store.LoadIndexSource(ctx, userID, indexID)
	if err != nil {
		return nil, errors.New("cannot read your saved constituent list")
	}
	if raw == "" {
		return nil, nil
	}
	var s indexsource.Snapshot
	if json.Unmarshal([]byte(raw), &s) != nil || s.IndexID != indexID || len(s.Members) == 0 {
		return nil, nil // damaged data is ignored, not guessed at
	}
	return &s, nil
}

// snapshotFor resolves an index for the caller from the context.
func (e *Engine) snapshotFor(ctx context.Context, idx indexDefinition) (*indexsource.Snapshot, error) {
	id, ok := e.indexUserID(ctx)
	if !ok {
		return nil, nil
	}
	return e.userSnapshot(ctx, id, idx.ID)
}

func indexSpec(d indexDefinition) indexsource.Spec {
	return indexsource.Spec{ID: d.ID, Name: d.Name, Country: d.Country, Nominal: d.NominalCount}
}

// unavailableForUser is the reason shown, and the 422 body's reason, when the
// caller has supplied no list for the index.
func unavailableForUser(d indexDefinition) string {
	how := "Import a CSV file of its constituents"
	if _, ok := indexsource.FMPIndexes[d.ID]; ok {
		how += " or fetch them with your own Financial Modeling Prep key"
	}
	return "You have not supplied a constituent list for " + d.Name + ". FloweySales ships none. " + how + " (Leaderboard, Index row)."
}

// definitionWith turns the registry entry into what this user has: the same
// index, available, with the list and provenance they supplied.
func definitionWith(d indexDefinition, s *indexsource.Snapshot) indexDefinition {
	if s == nil {
		d.UnavailableWhy = unavailableForUser(d)
		return d
	}
	asOf, imported := s.AsOf, s.ImportedAt
	d.Status, d.UnavailableWhy, d.AsOf = indexAvailable, "", &asOf
	d.ConstituentSrc = indexConstituentSource{Name: s.SourceName + " (supplied by you via " + providerLabel(s.Provider) + ")", RetrievedAt: &imported, LicenseVerified: s.LicenseConfirm}
	d.Licensing = "Supplied by you. You confirmed: \"" + s.LicenseTerms + "\" FloweySales ships no constituent list; check the provider's terms yourself."
	d.History = indexHistory{PointInTime: s.PointInTime}
	if s.PointInTime {
		d.History.Note = "Past membership is known from " + s.HistoryFrom + " from the add/remove records you fetched; earlier dates fall back to today's members and are labeled."
	} else {
		d.History.Note = "Only today's members are known. Past membership is not available, so a backtest on this list is survivorship-biased."
	}
	d.Constituents = make([]indexConstituent, len(s.Members))
	for i, m := range s.Members {
		d.Constituents[i] = indexConstituent{Symbol: m.Symbol, Name: m.Name}
	}
	return d
}

func providerLabel(p string) string {
	if p == indexsource.ProviderFMP {
		return "Financial Modeling Prep"
	}
	return "CSV import"
}

// userSourceView is the provenance of a user's list, as the API shows it.
type userSourceView struct {
	Provider       string   `json:"provider"`
	SourceName     string   `json:"source_name"`
	AsOf           string   `json:"as_of"`
	ImportedAt     string   `json:"imported_at"`
	Count          int      `json:"count"`
	LicenseTerms   string   `json:"license_terms"`
	LicenseConfirm bool     `json:"license_confirmed"`
	PointInTime    bool     `json:"point_in_time"`
	HistoryFrom    string   `json:"history_from,omitempty"`
	Changes        int      `json:"change_records"`
	Unverified     bool     `json:"unverified,omitempty"`
	Warnings       []string `json:"warnings"`
}

// userIndexView is the registry view for one user: unavailable until they
// supply a list, then available with that list's provenance and warnings.
func userIndexView(d indexDefinition, s *indexsource.Snapshot, now time.Time) indexView {
	v := viewIndex(definitionWith(d, s), now)
	v.Constituents = []indexConstituent{} // the list stays out of the payload; count says how many
	v.Sources = []string{indexsource.ProviderCSV}
	if _, ok := indexsource.FMPIndexes[d.ID]; ok {
		v.Sources = append(v.Sources, indexsource.ProviderFMP)
	}
	if s == nil {
		v.Warnings = []string{"Unavailable: " + unavailableForUser(d)}
		return v
	}
	for _, w := range s.Warnings {
		if strings.Contains(w, "survivorship") || strings.Contains(w, "securities; the index name says") {
			continue // viewIndex already says these
		}
		v.Warnings = append(v.Warnings, w)
	}
	v.Source = &userSourceView{Provider: s.Provider, SourceName: s.SourceName, AsOf: s.AsOf, ImportedAt: s.ImportedAt, Count: len(s.Members),
		LicenseTerms: s.LicenseTerms, LicenseConfirm: s.LicenseConfirm, PointInTime: s.PointInTime, HistoryFrom: s.HistoryFrom,
		Changes: len(s.Changes), Unverified: s.Unverified, Warnings: s.Warnings}
	return v
}

func indexByRoute(w http.ResponseWriter, r *http.Request) (indexDefinition, bool) {
	d, ok := findIndex(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Unknown index"})
	}
	return d, ok
}

type indexCSVBody struct {
	CSV            string `json:"csv"`
	AsOf           string `json:"as_of"`
	SourceName     string `json:"source_name"`
	LicenseTerms   string `json:"license_terms"`
	LicenseConfirm bool   `json:"license_confirmed"`
}

type indexFetchBody struct {
	LicenseTerms   string `json:"license_terms"`
	LicenseConfirm bool   `json:"license_confirmed"`
}

func decodeStrict(w http.ResponseWriter, r *http.Request, limit int64, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body (or it is too large)"})
		return false
	}
	return true
}

func (e *Engine) saveSnapshot(w http.ResponseWriter, r *http.Request, userID int64, d indexDefinition, s indexsource.Snapshot) {
	store := e.indexSourceStore()
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Storage unavailable"})
		return
	}
	raw, _ := json.Marshal(s)
	if len(raw) > storage.IndexSourceMaxBytes {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "The list is too large to store"})
		return
	}
	if err := store.SaveIndexSource(r.Context(), userID, d.ID, string(raw)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot save the constituent list"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "index": userIndexView(d, &s, time.Now().UTC())})
}

func refusal(w http.ResponseWriter, status int, message string, problems []string) {
	if len(problems) > 50 {
		problems = append(problems[:50:50], fmt.Sprintf("... and %d more", len(problems)-50))
	}
	writeJSON(w, status, map[string]any{"error": message, "problems": problems})
}

// registerIndexSourceRoutes serves the caller's own constituent data. Every
// route is scoped to the signed-in user; none can read or write another user's
// list or key, and the key is never returned.
func (e *Engine) registerIndexSourceRoutes() {
	e.mux.HandleFunc("PUT /api/market/indices/{id}/csv", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		d, ok := indexByRoute(w, r)
		if !ok {
			return
		}
		var body indexCSVBody
		if !decodeStrict(w, r, indexsource.MaxCSVBytes+8192, &body) {
			return
		}
		snap, problems, err := indexsource.ParseCSV([]byte(body.CSV), indexSpec(d), indexsource.Meta{AsOf: body.AsOf, SourceName: body.SourceName,
			LicenseTerms: body.LicenseTerms, LicenseConfirm: body.LicenseConfirm}, time.Now().UTC())
		if err != nil {
			refusal(w, http.StatusUnprocessableEntity, "The file was not imported", problems)
			return
		}
		e.saveSnapshot(w, r, u.ID, d, snap)
	})
	e.mux.HandleFunc("POST /api/market/indices/{id}/fmp", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		d, ok := indexByRoute(w, r)
		if !ok {
			return
		}
		if _, supported := indexsource.FMPIndexes[d.ID]; !supported {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": d.Name + " has no Financial Modeling Prep source here. Import a CSV file instead."})
			return
		}
		var body indexFetchBody
		if !decodeStrict(w, r, 8192, &body) {
			return
		}
		key, status, msg := e.userFMPKey(r.Context(), u.ID)
		if key == "" {
			writeJSON(w, status, map[string]any{"error": msg})
			return
		}
		fmp := indexsource.FMP{Client: e.indexHTTPClient(), Base: e.indexFMPBase, Key: key}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		snap, problems, err := fmp.Fetch(ctx, indexSpec(d), indexsource.Meta{LicenseTerms: body.LicenseTerms, LicenseConfirm: body.LicenseConfirm}, time.Now().UTC())
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, indexsource.ErrFMPAuth) {
				status = http.StatusBadGateway
			}
			refusal(w, status, "The list was not fetched", problems)
			return
		}
		e.saveSnapshot(w, r, u.ID, d, snap)
	})
	e.mux.HandleFunc("DELETE /api/market/indices/{id}/source", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		d, ok := indexByRoute(w, r)
		if !ok {
			return
		}
		store := e.indexSourceStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Storage unavailable"})
			return
		}
		if err := store.DeleteIndexSource(r.Context(), u.ID, d.ID); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot remove the list"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "index": userIndexView(d, nil, time.Now().UTC())})
	})
	e.mux.HandleFunc("GET /api/market/indices/fmp-key", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		_, cerr := apiSecretCipher()
		writeJSON(w, http.StatusOK, map[string]any{"has_key": e.hasFMPKey(r.Context(), u.ID), "encryption_ready": cerr == nil})
	})
	e.mux.HandleFunc("PUT /api/market/indices/fmp-key", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		var body struct {
			APIKey string `json:"api_key"`
		}
		if !decodeStrict(w, r, 8192, &body) {
			return
		}
		key := strings.TrimSpace(body.APIKey)
		if key == "" || len(key) > 256 || strings.ContainsAny(key, " \t\r\n&?=/#") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid API key"})
			return
		}
		store := e.indexSecretStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Storage unavailable"})
			return
		}
		sealed, err := sealAPISecret(indexKeyKind(u.ID), key)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Credential encryption is unavailable; the server operator must configure the secrets key"})
			return
		}
		if err := store.SaveAPIIntegrationSecret(r.Context(), indexKeyKind(u.ID), "fmp", sealed); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot save the key"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "has_key": true})
	})
	e.mux.HandleFunc("DELETE /api/market/indices/fmp-key", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if store := e.indexSecretStore(); store != nil {
			if err := store.DeleteAPIIntegrationSecret(r.Context(), indexKeyKind(u.ID)); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot remove the key"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "has_key": false})
	})
}

func (e *Engine) hasFMPKey(ctx context.Context, userID int64) bool {
	store := e.indexSecretStore()
	if store == nil {
		return false
	}
	_, err := store.GetAPIIntegrationSecret(ctx, indexKeyKind(userID))
	return err == nil
}

// userFMPKey opens the caller's own key. The key never leaves this call
// except to be sent to FMP.
func (e *Engine) userFMPKey(ctx context.Context, userID int64) (key string, status int, msg string) {
	store := e.indexSecretStore()
	if store == nil {
		return "", http.StatusServiceUnavailable, "Storage unavailable"
	}
	entry, err := store.GetAPIIntegrationSecret(ctx, indexKeyKind(userID))
	if err != nil {
		return "", http.StatusBadRequest, "Save your Financial Modeling Prep key first"
	}
	key, err = openAPISecret(indexKeyKind(userID), entry.Ciphertext)
	if err != nil {
		return "", http.StatusServiceUnavailable, "Your saved key cannot be decrypted; save it again"
	}
	return key, 0, ""
}

// universesFor is the shared universes plus the index lists this caller has
// supplied, which only they can see.
func (e *Engine) universesFor(ctx context.Context) []leaderboardUniverse {
	out := append([]leaderboardUniverse(nil), leaderboardUniverses...)
	for _, d := range indexRegistry {
		if snap, err := e.snapshotFor(ctx, d); err == nil && snap != nil {
			out = append(out, definitionWith(d, snap).universe())
		}
	}
	return out
}
