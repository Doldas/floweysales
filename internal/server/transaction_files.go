package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"floweysales/internal/plugins"
	"floweysales/internal/storage"
)

// A user's saved transaction exports (broker CSV). They are encrypted with the
// server secret key, belong to one user and are only read when that user, or
// an agent acting for them, runs a domain operation whose input is sourced
// from them. Nothing here is shared with other users or logged.

// transactionStore keeps each user's saved transaction exports.
type transactionStore interface {
	ListTransactionFiles(ctx context.Context, userID int64) ([]storage.TransactionFile, error)
	TransactionFileContents(ctx context.Context, userID int64) ([]storage.SealedTransactionFile, error)
	SaveTransactionFile(ctx context.Context, userID int64, name string, sizeBytes int, ciphertext []byte) (int64, error)
	DeleteTransactionFile(ctx context.Context, userID, id int64) error
}

func (e *Engine) transactionStore() transactionStore {
	if e.transactions != nil {
		return e.transactions
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

// isTransactionRoute matches a user's own saved transaction files.
func isTransactionRoute(method, path string) bool {
	if path != "/api/me/transactions" && !strings.HasPrefix(path, "/api/me/transactions/") {
		return false
	}
	return method == http.MethodGet || method == http.MethodPost || method == http.MethodDelete
}

func transactionKind(userID int64, name string) string {
	return "transactions:" + strconv.FormatInt(userID, 10) + ":" + name
}

// cleanTransactionName keeps a file's base name: no paths, no control characters.
func cleanTransactionName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len([]rune(out)) > 120 {
		out = string([]rune(out)[:120])
	}
	return out
}

// savedTransactions returns the user's saved exports as one multi-file input
// and the names of the files in it.
func (e *Engine) savedTransactions(ctx context.Context, userID int64) (string, []string, error) {
	store := e.transactionStore()
	if store == nil {
		return "", nil, nil
	}
	files, err := store.TransactionFileContents(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	names := []string{}
	for _, f := range files {
		text, err := openAPISecret(transactionKind(userID, f.Name), f.Ciphertext)
		if err != nil {
			return "", nil, fmt.Errorf("saved transaction file %q cannot be read: %w", f.Name, err)
		}
		b.WriteString(plugins.DomainFileMarker + f.Name + " ===\n")
		b.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteString("\n")
		}
		names = append(names, f.Name)
	}
	return b.String(), names, nil
}

// sourcedFields returns the inputs of an operation that name a source.
func sourcedFields(list []plugins.DomainProvider, providerID, operation string) []plugins.DomainField {
	var out []plugins.DomainField
	for _, p := range list {
		if !strings.EqualFold(p.ProviderID, strings.TrimSpace(providerID)) {
			continue
		}
		for _, op := range p.Operations {
			if op.Capability != operation {
				continue
			}
			for _, f := range op.Inputs {
				if f.Source == plugins.SourceUserTransactions {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// fillSaved fills the sourced inputs the caller left empty from the saved
// data load returns. It returns the input to use (a copy when it changed,
// never the caller's map) and the names of the files used.
func fillSaved(fields []plugins.DomainField, input map[string]any, load func() (string, []string, error)) (map[string]any, []string, error) {
	out := input
	var used []string
	for _, f := range fields {
		if v, ok := input[f.Name]; ok && v != nil && v != "" {
			continue
		}
		text, names, err := load()
		if err != nil {
			return nil, nil, err
		}
		if len(names) == 0 {
			continue
		}
		if len(text) > plugins.MaxDomainInputBytes/2 {
			return nil, nil, errors.New("the saved transaction files are too large for one calculation")
		}
		copied := make(map[string]any, len(out)+1)
		for k, v := range out {
			copied[k] = v
		}
		copied[f.Name] = text
		out, used = copied, names
	}
	return out, used, nil
}

// withSavedInputs fills an operation's sourced inputs from the caller's own
// saved transaction files; see fillSaved.
func (e *Engine) withSavedInputs(ctx context.Context, userID int64, providerID, operation string, input map[string]any) (map[string]any, []string, error) {
	if e.plugins == nil {
		return input, nil, nil
	}
	fields := sourcedFields(e.plugins.DomainProviders(""), providerID, operation)
	return fillSaved(fields, input, func() (string, []string, error) { return e.savedTransactions(ctx, userID) })
}

func (e *Engine) registerTransactionRoutes() {
	limits := map[string]any{"max_files": storage.TransactionFilesMaxCount, "max_file_bytes": storage.TransactionFileMaxBytes, "max_total_bytes": storage.TransactionFilesMaxTotal}
	e.mux.HandleFunc("GET /api/me/transactions", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		files := []storage.TransactionFile{}
		if store := e.transactionStore(); store != nil {
			list, err := store.ListTransactionFiles(r.Context(), u.ID)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Saved transactions unavailable"})
				return
			}
			files = list
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files, "limits": limits})
	})

	e.mux.HandleFunc("POST /api/me/transactions", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		store := e.transactionStore()
		if !ok {
			return
		}
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Saved transactions need the database"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, storage.TransactionFileMaxBytes+64<<10)
		var body struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Send JSON {\"name\": \"...\", \"content\": \"...\"} of at most 2 MB"})
			return
		}
		name := cleanTransactionName(body.Name)
		switch {
		case name == "":
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A file name is required"})
			return
		case strings.TrimSpace(body.Content) == "":
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The file is empty"})
			return
		case len(body.Content) > storage.TransactionFileMaxBytes:
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "The file is larger than 2 MB"})
			return
		case !utf8.ValidString(body.Content) || strings.ContainsRune(body.Content, 0):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The file must be text (CSV)"})
			return
		case strings.Contains(body.Content, plugins.DomainFileMarker):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The file contains a reserved marker line"})
			return
		}
		sealed, err := sealAPISecret(transactionKind(u.ID, name), body.Content)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Server secret encryption key is not configured"})
			return
		}
		id, err := store.SaveTransactionFile(r.Context(), u.ID, name, len(body.Content), sealed)
		switch {
		case errors.Is(err, storage.ErrTransactionFileLimit):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		case err != nil:
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Saved transactions unavailable"})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name, "size_bytes": len(body.Content)})
		}
	})

	e.mux.HandleFunc("DELETE /api/me/transactions/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		store := e.transactionStore()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if !ok {
			return
		}
		if err != nil || id <= 0 || store == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "File not found"})
			return
		}
		if err := store.DeleteTransactionFile(r.Context(), u.ID, id); err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, storage.ErrTransactionFileNotFound) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
	})
}
