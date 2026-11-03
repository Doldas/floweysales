package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/plugins"
	"floweysales/internal/storage"
)

type memoryTransactions struct {
	mu   sync.Mutex
	next int64
	rows map[int64]map[int64]storage.SealedTransactionFile // user → id → file
}

func (m *memoryTransactions) ListTransactionFiles(_ context.Context, u int64) ([]storage.TransactionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.TransactionFile{}
	for _, f := range m.rows[u] {
		out = append(out, f.TransactionFile)
	}
	return out, nil
}
func (m *memoryTransactions) TransactionFileContents(_ context.Context, u int64) ([]storage.SealedTransactionFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.SealedTransactionFile{}
	for id := int64(1); id <= m.next; id++ {
		if f, ok := m.rows[u][id]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}
func (m *memoryTransactions) SaveTransactionFile(_ context.Context, u int64, name string, size int, ct []byte) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows[u] == nil {
		m.rows[u] = map[int64]storage.SealedTransactionFile{}
	}
	for id, f := range m.rows[u] {
		if f.Name == name {
			m.rows[u][id] = storage.SealedTransactionFile{TransactionFile: storage.TransactionFile{ID: id, Name: name, SizeBytes: size, CreatedAt: time.Now()}, Ciphertext: ct}
			return id, nil
		}
	}
	if len(m.rows[u]) >= storage.TransactionFilesMaxCount {
		return 0, storage.ErrTransactionFileLimit
	}
	m.next++
	m.rows[u][m.next] = storage.SealedTransactionFile{TransactionFile: storage.TransactionFile{ID: m.next, Name: name, SizeBytes: size, CreatedAt: time.Now()}, Ciphertext: ct}
	return m.next, nil
}
func (m *memoryTransactions) DeleteTransactionFile(_ context.Context, u, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[u][id]; !ok {
		return storage.ErrTransactionFileNotFound
	}
	delete(m.rows[u], id)
	return nil
}

func transactionEngine(t *testing.T) (*Engine, *memoryTransactions) {
	t.Helper()
	t.Setenv("FLOWEYSALES_SECRETS_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	e := teamEngine()
	store := &memoryTransactions{rows: map[int64]map[int64]storage.SealedTransactionFile{}}
	e.transactions = store
	return e, store
}

func callRoute(e *Engine, method, path, body string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestSavedTransactionsAreEncryptedListedAndDeleted(t *testing.T) {
	e, store := transactionEngine(t)
	code, out := callRoute(e, http.MethodPost, "/api/me/transactions", `{"name":"C:\\exports\\avanza 2025.csv","content":"date;type\n2025-01-01;Köp\n"}`)
	if code != http.StatusOK || out["name"] != "avanza 2025.csv" {
		t.Fatalf("%d %v", code, out)
	}
	for _, f := range store.rows[0] {
		if strings.Contains(string(f.Ciphertext), "Köp") {
			t.Fatal("the content must be stored encrypted")
		}
	}
	text, names, err := e.savedTransactions(context.Background(), 0)
	if err != nil || len(names) != 1 || !strings.HasPrefix(text, plugins.DomainFileMarker+"avanza 2025.csv ===\n") || !strings.Contains(text, "2025-01-01;Köp") {
		t.Fatalf("text=%q names=%v err=%v", text, names, err)
	}
	if _, other, _ := e.savedTransactions(context.Background(), 7); len(other) != 0 {
		t.Fatal("another user must not see these files")
	}
	code, out = callRoute(e, http.MethodGet, "/api/me/transactions", "")
	files, _ := out["files"].([]any)
	if code != http.StatusOK || len(files) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if strings.Contains(strings.ToLower(toJSON(out)), "köp") {
		t.Fatal("the list must not carry content")
	}
	id := int64(files[0].(map[string]any)["id"].(float64))
	if code, _ := callRoute(e, http.MethodDelete, "/api/me/transactions/"+itoa(id), ""); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := callRoute(e, http.MethodDelete, "/api/me/transactions/"+itoa(id), ""); code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
}

func TestSavedTransactionsRefuseBadFiles(t *testing.T) {
	e, _ := transactionEngine(t)
	for name, body := range map[string]string{
		"no name":      `{"name":" ","content":"a"}`,
		"empty":        `{"name":"a.csv","content":"  "}`,
		"binary":       `{"name":"a.csv","content":"a\u0000b"}`,
		"marker":       `{"name":"a.csv","content":"x\n=== file: other ===\ny"}`,
		"not json":     `nope`,
		"large":        `{"name":"a.csv","content":"` + strings.Repeat("x", storage.TransactionFileMaxBytes+1) + `"}`,
		"unknown body": `[]`,
	} {
		if code, _ := callRoute(e, http.MethodPost, "/api/me/transactions", body); code < 400 {
			t.Errorf("%s: accepted with %d", name, code)
		}
	}
}

func TestCleanTransactionName(t *testing.T) {
	for in, want := range map[string]string{"a.csv": "a.csv", " ../../etc/passwd ": "passwd", `C:\x\y.csv`: "y.csv", "a\x00b.csv": "ab.csv", "": ""} {
		if got := cleanTransactionName(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func itoa(i int64) string { b, _ := json.Marshal(i); return string(b) }
func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestSavedFilesFillOnlyEmptySourcedInputs(t *testing.T) {
	list := []plugins.DomainProvider{{ProviderID: "SE_TAX", Operations: []plugins.DomainOperation{
		{Capability: "tax_report", Inputs: []plugins.DomainField{
			{Name: "year", Type: plugins.FieldInteger},
			{Name: "csv", Type: plugins.FieldString, Source: plugins.SourceUserTransactions}}},
		{Capability: "tax_estimate"}}}}
	fields := sourcedFields(list, "se_tax", "tax_report")
	if len(fields) != 1 || fields[0].Name != "csv" || len(sourcedFields(list, "SE_TAX", "tax_estimate")) != 0 || len(sourcedFields(list, "OTHER", "tax_report")) != 0 {
		t.Fatalf("fields = %+v", fields)
	}
	load := func() (string, []string, error) { return "=== file: a.csv ===\nx\n", []string{"a.csv"}, nil }
	in := map[string]any{"year": 2025}
	out, used, err := fillSaved(fields, in, load)
	if err != nil || out["csv"] == nil || len(used) != 1 || used[0] != "a.csv" {
		t.Fatalf("out=%v used=%v err=%v", out, used, err)
	}
	if _, wrote := in["csv"]; wrote {
		t.Fatal("the caller's map must not be changed")
	}
	given := map[string]any{"csv": "pasted"}
	out, used, _ = fillSaved(fields, given, load)
	if out["csv"] != "pasted" || len(used) != 0 {
		t.Fatalf("a pasted value wins over saved files: %v %v", out, used)
	}
	none := func() (string, []string, error) { return "", nil, nil }
	if out, used, _ = fillSaved(fields, in, none); out["csv"] != nil || len(used) != 0 {
		t.Fatalf("nothing saved, nothing filled: %v", out)
	}
}
