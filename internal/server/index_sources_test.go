package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/indexsource"
	"floweysales/internal/storage"
)

type memIndexSources struct{ saved map[string]string }

func (m *memIndexSources) k(u int64, id string) string { return fmt.Sprintf("%d/%s", u, id) }
func (m *memIndexSources) LoadIndexSource(_ context.Context, u int64, id string) (string, error) {
	return m.saved[m.k(u, id)], nil
}
func (m *memIndexSources) SaveIndexSource(_ context.Context, u int64, id, d string) error {
	m.saved[m.k(u, id)] = d
	return nil
}
func (m *memIndexSources) DeleteIndexSource(_ context.Context, u int64, id string) error {
	delete(m.saved, m.k(u, id))
	return nil
}

type memSecrets struct{ saved map[string][]byte }

func (m *memSecrets) SaveAPIIntegrationSecret(_ context.Context, kind, _ string, ct []byte) error {
	m.saved[kind] = ct
	return nil
}
func (m *memSecrets) GetAPIIntegrationSecret(_ context.Context, kind string) (storage.APIIntegrationSecret, error) {
	ct, ok := m.saved[kind]
	if !ok {
		return storage.APIIntegrationSecret{}, sql.ErrNoRows
	}
	return storage.APIIntegrationSecret{Provider: "fmp", Ciphertext: ct}, nil
}
func (m *memSecrets) DeleteAPIIntegrationSecret(_ context.Context, kind string) error {
	delete(m.saved, kind)
	return nil
}

// fmpFixture answers FMP paths from recorded-style fixtures; it records the
// URLs it was asked for. No test here reaches the network.
type fmpFixture struct {
	urls []string
	cur  string
	hist string
}

func (f *fmpFixture) Do(r *http.Request) (*http.Response, error) {
	f.urls = append(f.urls, r.URL.String())
	body := `{}`
	switch strings.TrimPrefix(r.URL.Path, "/stable/") {
	case "sp500-constituent":
		body = f.cur
	case "historical-sp500-constituent":
		body = f.hist
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func sp500Fixture(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"symbol":"UP%03d","name":"Co %d"}`, i, i)
	}
	parts[0] = `{"symbol":"UP","name":"Up Inc"}`
	parts[1] = `{"symbol":"DOWN","name":"Down Inc"}`
	return "[" + strings.Join(parts, ",") + "]"
}

const sp500History = `[{"symbol":"UP","removedTicker":"OLDCO","date":"2025-06-02","dateAdded":"June 2, 2025"},{"symbol":"DOWN","removedTicker":"GONE","date":"2024-01-02"}]`

func indexTestEngine(t *testing.T) (*Engine, *fmpFixture) {
	t.Helper()
	t.Setenv(apiSecretEnv, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	e := investorEngine(t)
	e.indexSources = &memIndexSources{saved: map[string]string{}}
	e.indexSecrets = &memSecrets{saved: map[string][]byte{}}
	fx := &fmpFixture{cur: sp500Fixture(500), hist: sp500History}
	e.indexHTTP = fx
	return e, fx
}

func icall(e *Engine, method, path string, body any) (int, map[string]any, string) {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, rd))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

func omxsCSV(n int) string {
	var b strings.Builder
	b.WriteString("symbol,name,weight,exchange\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "SW%02d,Swede %d,,ST\n", i, i)
	}
	return b.String()
}

func csvBody(csv string) map[string]any {
	return map[string]any{"csv": csv, "as_of": time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"), "source_name": "My own export",
		"license_terms": "I hold a licence for this file.", "license_confirmed": true}
}

func indexByID(t *testing.T, out map[string]any, id string) map[string]any {
	t.Helper()
	for _, raw := range out["indices"].([]any) {
		if ix := raw.(map[string]any); ix["id"] == id {
			return ix
		}
	}
	t.Fatalf("index %s missing", id)
	return nil
}

func TestIndexIsUnavailableUntilUserSuppliesData(t *testing.T) {
	e, _ := indexTestEngine(t)
	_, out, _ := icall(e, "GET", "/api/market/indices", nil)
	ix := indexByID(t, out, "index_omxs30")
	if ix["available"] != false || ix["source"] != nil || !strings.Contains(fmt.Sprint(ix["warnings"]), "FloweySales ships none") {
		t.Fatalf("expected unavailable with the reason: %v", ix)
	}
	if fmt.Sprint(ix["sources"]) != "[csv]" {
		t.Errorf("OMXS30 is CSV only: %v", ix["sources"])
	}
	if got := fmt.Sprint(indexByID(t, out, "index_sp500")["sources"]); got != "[csv fmp]" {
		t.Errorf("S&P 500 sources: %s", got)
	}
	code, body, _ := icall(e, "GET", "/api/market/leaderboard?universe=index_omxs30&strategy=momentum", nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(body["error"]), "unavailable") {
		t.Fatalf("no data must be 422 with the reason: %d %v", code, body)
	}
	code, body, _ = icall(e, "GET", "/api/market/backtest?universe=index_omxs30&strategy=momentum", nil)
	if code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body["error"]), "You have not supplied") {
		t.Fatalf("backtest without data: %d %v", code, body)
	}
}

func TestCSVImportMakesIndexAvailableForThatUser(t *testing.T) {
	e, _ := indexTestEngine(t)
	code, out, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", csvBody(omxsCSV(30)))
	if code != 200 {
		t.Fatalf("import: %d %v", code, out)
	}
	_, list, _ := icall(e, "GET", "/api/market/indices", nil)
	ix := indexByID(t, list, "index_omxs30")
	src := ix["source"].(map[string]any)
	if ix["available"] != true || ix["count"].(float64) != 30 || src["provider"] != "csv" || src["license_confirmed"] != true ||
		src["point_in_time"] != false || src["source_name"] != "My own export" || ix["age_days"].(float64) != 3 {
		t.Fatalf("view: %v", ix)
	}
	if !strings.Contains(fmt.Sprint(ix["warnings"]), "survivorship") {
		t.Errorf("survivorship warning expected: %v", ix["warnings"])
	}
	if len(ix["constituents"].([]any)) != 0 {
		t.Error("the list itself stays out of the registry payload")
	}
	// Another user sees nothing: the store is keyed by user.
	other, err := e.userSnapshot(context.Background(), 99, "index_omxs30")
	if err != nil || other != nil {
		t.Fatalf("user 99 must not see user 0's list: %v %v", other, err)
	}
	mine, _ := e.userSnapshot(context.Background(), 0, "index_omxs30")
	if mine == nil || len(mine.Members) != 30 || mine.Members[0].Symbol != "SW00.ST" {
		t.Fatalf("stored: %+v", mine)
	}
	// Delete returns it to unavailable.
	if code, _, _ := icall(e, "DELETE", "/api/market/indices/index_omxs30/source", nil); code != 200 {
		t.Fatal("delete failed")
	}
	_, list, _ = icall(e, "GET", "/api/market/indices", nil)
	if indexByID(t, list, "index_omxs30")["available"] != false {
		t.Error("deleting the list must make the index unavailable again")
	}
}

func TestStaleListWarns(t *testing.T) {
	e, _ := indexTestEngine(t)
	body := csvBody(omxsCSV(30))
	body["as_of"] = time.Now().UTC().AddDate(0, 0, -200).Format("2006-01-02")
	if code, out, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", body); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	_, list, _ := icall(e, "GET", "/api/market/indices", nil)
	ix := indexByID(t, list, "index_omxs30")
	if ix["stale"] != true || !strings.Contains(fmt.Sprint(ix["warnings"]), "200 days old") {
		t.Fatalf("stale: %v", ix)
	}
}

func TestCSVRefusalIsUnprocessableAndStoresNothing(t *testing.T) {
	e, _ := indexTestEngine(t)
	dup := omxsCSV(30) + "SW01,Again,,ST\n"
	code, out, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", csvBody(dup))
	if code != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(out["problems"]), "appears twice") {
		t.Fatalf("duplicates: %d %v", code, out)
	}
	if code, _, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", csvBody(omxsCSV(5))); code != http.StatusUnprocessableEntity {
		t.Errorf("a list far from 30 must be refused: %d", code)
	}
	noAck := csvBody(omxsCSV(30))
	noAck["license_confirmed"] = false
	if code, _, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", noAck); code != http.StatusUnprocessableEntity {
		t.Errorf("without the licence confirmation: %d", code)
	}
	if code, _, _ := icall(e, "PUT", "/api/market/indices/index_nope/csv", csvBody("symbol\nA\n")); code != 404 {
		t.Errorf("unknown index: %d", code)
	}
	bad := csvBody(omxsCSV(30))
	bad["surprise"] = 1
	if code, _, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", bad); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
	if len(e.indexSources.(*memIndexSources).saved) != 0 {
		t.Error("a refused file must store nothing")
	}
}

func TestFMPKeyIsSealedAndNeverReturned(t *testing.T) {
	e, fx := indexTestEngine(t)
	const key = "k3y-SECRET-value"
	code, put, raw := icall(e, "PUT", "/api/market/indices/fmp-key", map[string]any{"api_key": key})
	if code != 200 || strings.Contains(raw, key) {
		t.Fatalf("save: %d %s", code, raw)
	}
	_ = put
	stored := e.indexSecrets.(*memSecrets).saved[indexKeyKind(0)]
	if len(stored) == 0 || bytes.Contains(stored, []byte(key)) {
		t.Fatal("the key must be stored encrypted")
	}
	if code, got, raw := icall(e, "GET", "/api/market/indices/fmp-key", nil); code != 200 || got["has_key"] != true || strings.Contains(raw, key) {
		t.Fatalf("status: %d %s", code, raw)
	}
	if code, _, _ := icall(e, "PUT", "/api/market/indices/fmp-key", map[string]any{"api_key": "a b&c"}); code != 400 {
		t.Errorf("a malformed key: %d", code)
	}

	code, out, raw := icall(e, "POST", "/api/market/indices/index_sp500/fmp", map[string]any{"license_terms": "I accept FMP's terms for my plan.", "license_confirmed": true})
	if code != 200 || strings.Contains(raw, key) {
		t.Fatalf("fetch: %d %s", code, raw)
	}
	if len(fx.urls) != 2 || !strings.Contains(fx.urls[0], "apikey="+url.QueryEscape(key)) {
		t.Fatalf("FMP requests: %v", fx.urls)
	}
	_, list, raw := icall(e, "GET", "/api/market/indices", nil)
	ix := indexByID(t, list, "index_sp500")
	src := ix["source"].(map[string]any)
	if ix["available"] != true || src["provider"] != "fmp" || src["point_in_time"] != true || src["history_from"] != "2024-01-02" || src["unverified"] == true || strings.Contains(raw, key) {
		t.Fatalf("view: %v", ix)
	}
	_ = out
	// Without the licence confirmation nothing is fetched.
	fx.urls = nil
	if code, _, _ := icall(e, "POST", "/api/market/indices/index_sp500/fmp", map[string]any{"license_terms": "x", "license_confirmed": false}); code != 422 || len(fx.urls) != 0 {
		t.Errorf("unconfirmed fetch: %d %v", code, fx.urls)
	}
	// OMXS30 has no FMP source.
	if code, out, _ := icall(e, "POST", "/api/market/indices/index_omxs30/fmp", map[string]any{"license_terms": "long enough text", "license_confirmed": true}); code != 422 || !strings.Contains(fmt.Sprint(out["error"]), "CSV") {
		t.Errorf("omxs30 fmp: %d %v", code, out)
	}
	// Removing the key.
	if code, _, _ := icall(e, "DELETE", "/api/market/indices/fmp-key", nil); code != 200 {
		t.Fatal("delete key")
	}
	if code, out, _ := icall(e, "POST", "/api/market/indices/index_sp500/fmp", map[string]any{"license_terms": "I accept FMP's terms for my plan.", "license_confirmed": true}); code != 400 || !strings.Contains(fmt.Sprint(out["error"]), "key first") {
		t.Errorf("fetch without a key: %d %v", code, out)
	}
}

func TestFMPFetchWithoutEncryptionKeyIsRefused(t *testing.T) {
	e, _ := indexTestEngine(t)
	t.Setenv(apiSecretEnv, "")
	if code, _, _ := icall(e, "PUT", "/api/market/indices/fmp-key", map[string]any{"api_key": "abc"}); code != http.StatusServiceUnavailable {
		t.Errorf("the key must not be stored in the clear: %d", code)
	}
}

func TestLeaderboardAndBacktestUseTheUsersIndex(t *testing.T) {
	e, _ := indexTestEngine(t)
	icall(e, "PUT", "/api/market/indices/fmp-key", map[string]any{"api_key": "abc123"})
	if code, out, _ := icall(e, "POST", "/api/market/indices/index_sp500/fmp", map[string]any{"license_terms": "I accept FMP's terms for my plan.", "license_confirmed": true}); code != 200 {
		t.Fatalf("fetch: %d %v", code, out)
	}
	code, board, _ := icall(e, "GET", "/api/market/leaderboard?universe=index_sp500&strategy=momentum", nil)
	if code != 200 {
		t.Fatalf("leaderboard: %d %v", code, board)
	}
	info := board["universe_info"].(map[string]any)
	if info["id"] != "index_sp500" || info["source"].(map[string]any)["kind"] != "index" || info["as_of"] == "" {
		t.Errorf("universe info: %v", info)
	}
	found := false
	for _, u := range board["universes"].([]any) {
		if u.(map[string]any)["id"] == "index_sp500" {
			found = true
		}
	}
	if !found {
		t.Error("the user's index must be listed among their universes")
	}

	req, err := e.parseBacktestQuery(withIndexUser(context.Background(), 0), url.Values{"universe": {"index_sp500"}, "strategy": {"momentum"}, "years": {"3"}})
	if err != nil {
		t.Fatal(err)
	}
	// History reaches back to 2024-01-02 only, so the three-year window includes both removed tickers.
	if !containsString(req.symbols, "OLDCO") || !containsString(req.symbols, "GONE") || req.snap == nil || !strings.Contains(req.cacheKey, "idx:index_sp500") || req.benchmark != "^GSPC" {
		t.Errorf("request: %d symbols, snap=%v, key=%s, bench=%s", len(req.symbols), req.snap != nil, req.cacheKey, req.benchmark)
	}
	// Auth on and no user attached (a flow or agent run): the index is not available.
	e.authService = &identity.Service{}
	if _, err := e.parseBacktestQuery(context.Background(), url.Values{"universe": {"index_sp500"}, "strategy": {"momentum"}}); err == nil {
		t.Error("a caller with no user must not see anyone's index")
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestMembershipTrackDisclosure(t *testing.T) {
	snap := &indexsource.Snapshot{IndexID: "i", Provider: "fmp", AsOf: "2026-11-01", PointInTime: true, HistoryFrom: "2024-01-02",
		Members: []indexsource.Member{{Symbol: "A"}, {Symbol: "B"}},
		Changes: []indexsource.Change{{Date: "2025-01-01", Added: "B", Removed: "C"}}}
	tr := &membershipTrack{}
	at := func(d string) map[string]bool {
		ts, _ := time.Parse("2006-01-02", d)
		return tr.allowed(snap, ts)
	}
	if got := at("2024-06-01"); !got["C"] || got["B"] {
		t.Errorf("2024-06-01: %v", got)
	}
	if got := at("2026-01-01"); got["C"] || !got["B"] {
		t.Errorf("2026-01-01: %v", got)
	}
	if info := tr.info(snap, 0); info.Mode != membershipPointInTime || info.PITCount != 2 {
		t.Errorf("all point in time: %+v", info)
	}
	if got := at("2023-01-01"); got["C"] { // before the history: today's members stand in
		t.Errorf("fallback uses today's members: %v", got)
	}
	info := tr.info(snap, 3)
	if info.Mode != membershipPartialFalls || info.FallbackN != 1 || !strings.Contains(info.Note, "survivorship") || !strings.Contains(info.Note, "3 symbols had no price history") {
		t.Errorf("fallback must be disclosed: %+v", info)
	}
	snap.PointInTime = false
	if (&membershipTrack{}).allowed(snap, time.Now()) != nil {
		t.Error("without history no filtering happens")
	}
	if info := (&membershipTrack{}).info(snap, 0); info.Mode != membershipCurrentOnly || !strings.Contains(info.Note, "survivorship") {
		t.Errorf("current-only disclosure: %+v", info)
	}
	if (&membershipTrack{}).info(nil, 0) != nil {
		t.Error("a non-index universe has no membership info")
	}
}

func TestIndexSourceRoutesAreOwnSettingsForUsers(t *testing.T) {
	cases := []struct {
		role, method, path string
		want               bool
	}{
		{identity.RoleUser, "GET", "/api/market/indices", true},
		{identity.RoleUser, "PUT", "/api/market/indices/index_sp500/csv", true},
		{identity.RoleUser, "POST", "/api/market/indices/index_sp500/fmp", true},
		{identity.RoleUser, "DELETE", "/api/market/indices/index_sp500/source", true},
		{identity.RoleUser, "GET", "/api/market/indices/fmp-key", true},
		{identity.RoleUser, "PUT", "/api/market/indices/fmp-key", true},
		{identity.RoleUser, "DELETE", "/api/market/indices/fmp-key", true},
		{identity.RoleUser, "GET", "/api/market/indices/index_sp500/csv", false},
		{identity.RoleUser, "POST", "/api/market/indices/index_sp500/csv", false},
		{identity.RoleUser, "PUT", "/api/market/indices/index_sp500/fmp", false},
		{identity.RoleUser, "PUT", "/api/market/indices", false},
		{identity.RoleUser, "PUT", "/api/market/indices/index_sp500/other", false},
		{"HACKER", "PUT", "/api/market/indices/fmp-key", false},
		// The administrator sets up the system; the key is a user's, so no admin route holds it.
		{identity.RoleUser, "PUT", "/api/admin/integrations/fmp", false},
	}
	for _, c := range cases {
		if got := allowedAPI(c.role, c.method, c.path); got != c.want {
			t.Errorf("%s %s %s: got %v want %v", c.role, c.method, c.path, got, c.want)
		}
	}
}

func TestIndexSourceRoutesRequireSignIn(t *testing.T) {
	e, _ := indexTestEngine(t)
	e.authService = &identity.Service{}
	for _, c := range [][2]string{{"GET", "/api/market/indices"}, {"PUT", "/api/market/indices/index_sp500/csv"}, {"PUT", "/api/market/indices/fmp-key"}, {"POST", "/api/market/indices/index_sp500/fmp"}} {
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, httptest.NewRequest(c[0], c[1], strings.NewReader("{}")))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", c[0], c[1], rec.Code)
		}
	}
}
