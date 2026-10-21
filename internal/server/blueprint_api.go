package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"floweysales/internal/storage"
)

func (e *Engine) registerBlueprintRoutes() {
	e.mux.HandleFunc("GET /api/admin/blueprint-functions", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		defs, err := e.db.ListBlueprintFunctions(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Function storage unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"functions": defs})
	})
	e.mux.HandleFunc("POST /api/admin/blueprint-functions", func(w http.ResponseWriter, r *http.Request) { e.saveBlueprintFunction(w, r, 0) })
	e.mux.HandleFunc("PUT /api/admin/blueprint-functions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Invalid function ID"})
			return
		}
		e.saveBlueprintFunction(w, r, id)
	})
	e.mux.HandleFunc("GET /api/admin/blueprint-triggers", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		defs, err := e.db.ListBlueprintTriggers(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Trigger storage unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"triggers": defs})
	})
	e.mux.HandleFunc("POST /api/admin/blueprint-triggers", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()
		var t storage.BlueprintTrigger
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&t); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid trigger JSON"})
			return
		}
		var tail any
		if dec.Decode(&tail) != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "Unexpected data after trigger"})
			return
		}
		if !flowEventPattern.MatchString(t.EventKey) || (t.PayloadType != "text" && t.PayloadType != "number" && t.PayloadType != "boolean") {
			writeJSON(w, 400, map[string]string{"error": "Trigger requires valid event key and text/number/boolean payload type"})
			return
		}
		t.ID = 0
		user, _ := e.currentUser(r)
		saved, err := e.db.CreateBlueprintTrigger(r.Context(), t, user.ID)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "Trigger already exists or storage unavailable"})
			return
		}
		writeJSON(w, 201, saved)
	})
}
func (e *Engine) saveBlueprintFunction(w http.ResponseWriter, r *http.Request, id int64) {
	if !e.flowAdmin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20000)
	defer r.Body.Close()
	var definition storage.BlueprintFunction
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&definition); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid function JSON"})
		return
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "Unexpected data after function"})
		return
	}
	definition.ID = id
	if err := validateBlueprintFunction(definition); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	user, _ := e.currentUser(r)
	saved, err := e.db.SaveBlueprintFunction(r.Context(), definition, user.ID)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "Could not save function"})
		return
	}
	writeJSON(w, 200, saved)
}
