package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/identity"
)

// Market Collections: each user's own sets of instruments (internal/collections).
// Every handler works on the caller's rows only; another user's collection
// answers 404, the same as one that does not exist.

const (
	collectionBodyMax   = 32 * 1024
	collectionImportMax = 1 << 20
)

func (e *Engine) collectionService() (collections.Service, bool) {
	if e.collectionStore != nil {
		return collections.Service{Store: e.collectionStore}, true
	}
	if e.db != nil {
		return collections.Service{Store: e.db}, true
	}
	return collections.Service{}, false
}

// collectionErrorStatus maps domain errors to HTTP statuses. Anything else is
// a storage failure, reported without its details.
func writeCollectionError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	msg := "Collections are unavailable right now"
	switch {
	case errors.Is(err, collections.ErrNotFound), errors.Is(err, collections.ErrMemberNotFound):
		status, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, collections.ErrNameTaken), errors.Is(err, collections.ErrDuplicateMember):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, collections.ErrLimit), errors.Is(err, collections.ErrUnsupportedKind):
		status, msg = http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, collections.ErrInvalid):
		status, msg = http.StatusBadRequest, err.Error()
	default:
		log.Printf("collections: %v", err)
	}
	writeJSON(w, status, map[string]any{"error": msg})
}

func decodeCollectionBody(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request: " + err.Error()})
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid " + name})
		return 0, false
	}
	return id, true
}

func pageOf(r *http.Request) collections.Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return collections.Page{Limit: limit, Offset: offset}.Normalize()
}

// collectionRequest starts every collection handler: the caller and the service.
func (e *Engine) collectionRequest(w http.ResponseWriter, r *http.Request) (identity.User, collections.Service, bool) {
	u, ok := e.planUser(w, r)
	if !ok {
		return u, collections.Service{}, false
	}
	svc, ok := e.collectionService()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Collections need the database"})
		return u, svc, false
	}
	return u, svc, true
}

type collectionDraft struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
}

type memberDraft struct {
	Instrument collections.Instrument `json:"instrument"`
	Note       string                 `json:"note"`
	Tags       []string               `json:"tags"`
	Weight     *float64               `json:"weight"`
}

type membersPage struct {
	Members []collections.Member `json:"members"`
	Total   int                  `json:"total"`
	Limit   int                  `json:"limit"`
	Offset  int                  `json:"offset"`
}

// registerCollectionRoutes serves /api/collections.
func (e *Engine) registerCollectionRoutes() {
	e.mux.HandleFunc("GET /api/collections", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		f := collections.Filter{Query: q.Get("q"), Category: q.Get("category"), Tag: q.Get("tag"), Archive: collections.Archive(q.Get("archived")), Sort: q.Get("sort")}
		if k := q.Get("kind"); k != "" {
			f.Kind = collections.Kind(strings.ToLower(k))
		}
		list, err := svc.List(r.Context(), u.ID, f)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"collections": list, "limits": map[string]int{"collections": collections.MaxCollectionsPerUser, "members": collections.MaxMembers}})
	})

	e.mux.HandleFunc("POST /api/collections", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		var d collectionDraft
		if !decodeCollectionBody(w, r, collectionBodyMax, &d) {
			return
		}
		c, err := svc.Create(r.Context(), u.ID, collections.Collection{Name: d.Name, Description: d.Description, Kind: collections.Kind(d.Kind), Category: d.Category, Tags: d.Tags})
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	})

	e.mux.HandleFunc("GET /api/collections/containing", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		list, err := svc.Containing(r.Context(), u.ID, collections.Instrument{Provider: r.URL.Query().Get("provider"), Symbol: r.URL.Query().Get("symbol")})
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"collections": list})
	})

	e.mux.HandleFunc("GET /api/collections/export", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		var ids []int64
		for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
			if part = strings.TrimSpace(part); part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ids must be a comma-separated list of collection ids"})
				return
			}
			ids = append(ids, id)
		}
		doc, err := svc.Export(r.Context(), u.ID, ids, time.Now())
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="floweysales-collections.json"`)
		writeJSON(w, http.StatusOK, doc)
	})

	e.mux.HandleFunc("POST /api/collections/import", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		var doc collections.Document
		if !decodeCollectionBody(w, r, collectionImportMax, &doc) {
			return
		}
		results, err := svc.Import(r.Context(), u.ID, doc, collections.OnConflict(r.URL.Query().Get("on_conflict")))
		if err != nil {
			if len(results) == 0 {
				writeCollectionError(w, err)
				return
			}
			// Part of the file was saved before a storage failure: say what.
			log.Printf("collections import: %v", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The import stopped part way; the collections listed were saved", "results": results})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	})

	// from-trackers copies the system tracker list into a new watch collection:
	// the migration path from the single shared list. The tracker list is
	// system configuration, so only SYS_ADMIN reads it; it is left unchanged.
	e.mux.HandleFunc("POST /api/collections/from-trackers", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		if u.Role != identity.RoleSysAdmin {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Only a system administrator can read the tracker list"})
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if !decodeCollectionBody(w, r, collectionBodyMax, &body) {
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			body.Name = "Tracked instruments"
		}
		now := time.Now().UTC()
		c, err := svc.Create(r.Context(), u.ID, collections.Collection{Name: body.Name, Kind: collections.KindWatch,
			Description: fmt.Sprintf("Copied from the tracker list on %s UTC. The tracker list itself is unchanged.", now.Format("2006-01-02 15:04"))})
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		added, skipped := 0, []string{}
		for _, t := range e.config.Targets() {
			if _, err := svc.AddMember(r.Context(), u.ID, c.ID, collections.Member{Instrument: collections.Instrument{Provider: t.ProviderID, Symbol: t.Symbol}}); err != nil {
				skipped = append(skipped, t.ProviderID+":"+t.Symbol)
				continue
			}
			added++
		}
		if c, err = svc.Get(r.Context(), u.ID, c.ID); err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"collection": c, "added": added, "skipped": skipped})
	})

	e.mux.HandleFunc("GET /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		c, err := svc.Get(r.Context(), u.ID, id)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		page := pageOf(r)
		members, total, err := svc.Members(r.Context(), u.ID, id, page)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"collection": c, "members": membersPage{members, total, page.Limit, page.Offset}})
	})

	e.mux.HandleFunc("PATCH /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var p collections.Patch
		if !decodeCollectionBody(w, r, collectionBodyMax, &p) {
			return
		}
		c, err := svc.Update(r.Context(), u.ID, id, p)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})

	e.mux.HandleFunc("DELETE /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if err := svc.Delete(r.Context(), u.ID, id); err != nil {
			writeCollectionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	e.mux.HandleFunc("GET /api/collections/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		page := pageOf(r)
		members, total, err := svc.Members(r.Context(), u.ID, id, page)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, membersPage{members, total, page.Limit, page.Offset})
	})

	e.mux.HandleFunc("POST /api/collections/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var d memberDraft
		if !decodeCollectionBody(w, r, collectionBodyMax, &d) {
			return
		}
		m, err := svc.AddMember(r.Context(), u.ID, id, collections.Member{Instrument: d.Instrument, Note: d.Note, Tags: d.Tags, Weight: d.Weight})
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, m)
	})

	e.mux.HandleFunc("PATCH /api/collections/{id}/members/{member}", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		memberID, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		var p collections.MemberPatch
		if !decodeCollectionBody(w, r, collectionBodyMax, &p) {
			return
		}
		m, err := svc.UpdateMember(r.Context(), u.ID, id, memberID, p)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, m)
	})

	e.mux.HandleFunc("DELETE /api/collections/{id}/members/{member}", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		memberID, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		if err := svc.RemoveMember(r.Context(), u.ID, id, memberID); err != nil {
			writeCollectionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	e.mux.HandleFunc("POST /api/collections/{id}/members/{member}/transfer", func(w http.ResponseWriter, r *http.Request) {
		u, svc, ok := e.collectionRequest(w, r)
		if !ok {
			return
		}
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		memberID, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		var body struct {
			To     int64    `json:"to"`
			Move   bool     `json:"move"`
			Weight *float64 `json:"weight"`
		}
		if !decodeCollectionBody(w, r, collectionBodyMax, &body) {
			return
		}
		if body.To < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "to must name the target collection"})
			return
		}
		m, err := svc.Transfer(r.Context(), u.ID, id, memberID, body.To, body.Move, body.Weight)
		if err != nil {
			writeCollectionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, m)
	})
}

// isCollectionRoute matches a user's own Market Collections. from-trackers
// reads the system tracker list and stays SYS_ADMIN only.
func isCollectionRoute(method, path string) bool {
	if path != "/api/collections" && !strings.HasPrefix(path, "/api/collections/") || path == "/api/collections/from-trackers" {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}
