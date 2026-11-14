package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"floweysales/internal/collections"
	"floweysales/internal/market"
	"floweysales/internal/tracker"
)

func collectionEngine() *Engine {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.collectionStore = collections.NewMemoryStore()
	return e
}

func callCollections(t *testing.T, e *Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	return rec.Code, raw
}

func idOf(m map[string]any) string { return strconv.FormatInt(int64(m["id"].(float64)), 10) }

func TestCollectionJourneyOverHTTP(t *testing.T) {
	e := collectionEngine()
	code, tech := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Technology","category":"Sector","tags":["AI"]}`)
	if code != http.StatusCreated || tech["kind"] != "watch" {
		t.Fatalf("create: %d %v", code, tech)
	}
	_, med := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Medicine"}`)
	if code, _ := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"technology"}`); code != http.StatusConflict {
		t.Fatalf("duplicate name: %d", code)
	}
	if code, body := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Real","kind":"linked"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("linked views are not offered yet: %d %v", code, body)
	}
	if code, _ := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"X","owner":5}`); code != http.StatusBadRequest {
		t.Fatalf("unknown fields must be refused: %d", code)
	}

	base := "/api/collections/" + idOf(tech)
	// Instruments from different markets; nothing about them is required but provider and symbol.
	for _, in := range []string{
		`{"instrument":{"provider":"YAHOO","symbol":"NVDA","name":"NVIDIA","exchange":"NASDAQ","currency":"USD","type":"equity"}}`,
		`{"instrument":{"provider":"YAHOO","symbol":"ERIC-B.ST","exchange":"Stockholm","currency":"SEK"}}`,
		`{"instrument":{"provider":"BINANCE","symbol":"BTCUSDT","type":"crypto"}}`,
	} {
		if code, body := callCollections(t, e, http.MethodPost, base+"/members", in); code != http.StatusCreated {
			t.Fatalf("add %s: %d %v", in, code, body)
		}
	}
	if code, _ := callCollections(t, e, http.MethodPost, base+"/members", `{"instrument":{"provider":"yahoo","symbol":"nvda"}}`); code != http.StatusConflict {
		t.Fatalf("duplicate member: %d", code)
	}
	if code, _ := callCollections(t, e, http.MethodPost, base+"/members", `{"instrument":{"provider":"YAHOO","symbol":"AMD"},"weight":5}`); code != http.StatusBadRequest {
		t.Fatalf("weights in a watch collection: %d", code)
	}

	code, page := callCollections(t, e, http.MethodGet, base+"?limit=2", "")
	members := page["members"].(map[string]any)
	if code != http.StatusOK || members["total"].(float64) != 3 || len(members["members"].([]any)) != 2 {
		t.Fatalf("detail page: %d %v", code, page)
	}
	first := members["members"].([]any)[0].(map[string]any)

	// Copy NVDA into Medicine: the instrument is in both.
	code, copied := callCollections(t, e, http.MethodPost, base+"/members/"+idOf(first)+"/transfer", `{"to":`+idOf(med)+`}`)
	if code != http.StatusCreated {
		t.Fatalf("copy: %d %v", code, copied)
	}
	_, in := callCollections(t, e, http.MethodGet, "/api/collections/containing?provider=YAHOO&symbol=NVDA", "")
	if got := in["collections"].([]any); len(got) != 2 {
		t.Fatalf("containing = %v", got)
	}

	if code, body := callCollections(t, e, http.MethodPatch, base+"/members/"+idOf(first), `{"note":"Data centers","tags":["AI","chips"]}`); code != http.StatusOK || body["note"] != "Data centers" {
		t.Fatalf("annotate: %d %v", code, body)
	}
	if code, body := callCollections(t, e, http.MethodPatch, base, `{"archived":true,"description":"Long-term"}`); code != http.StatusOK || body["archived"] != true {
		t.Fatalf("archive: %d %v", code, body)
	}
	_, list := callCollections(t, e, http.MethodGet, "/api/collections", "")
	if got := list["collections"].([]any); len(got) != 1 {
		t.Fatalf("archived collections are hidden by default: %v", got)
	}
	_, list = callCollections(t, e, http.MethodGet, "/api/collections?archived=include&sort=members", "")
	if got := list["collections"].([]any); len(got) != 2 || got[0].(map[string]any)["name"] != "Technology" {
		t.Fatalf("include + sort by members: %v", got)
	}

	// Export, then import into the same account: a renamed copy, never a merge.
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/collections/export?ids="+idOf(tech), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	code, imported := callCollections(t, e, http.MethodPost, "/api/collections/import", rec.Body.String())
	res := imported["results"].([]any)[0].(map[string]any)
	if code != http.StatusOK || res["saved_as"] != "Technology (2)" || res["members"].(float64) != 3 {
		t.Fatalf("import: %d %v", code, imported)
	}
	if code, _ := callCollections(t, e, http.MethodGet, "/api/collections/export?ids=abc", ""); code != http.StatusBadRequest {
		t.Fatalf("bad ids: %d", code)
	}

	if code, _ := callCollections(t, e, http.MethodDelete, base+"/members/"+idOf(first), ""); code != http.StatusNoContent {
		t.Fatalf("remove member: %d", code)
	}
	if code, _ := callCollections(t, e, http.MethodDelete, base, ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := callCollections(t, e, http.MethodGet, base, ""); code != http.StatusNotFound {
		t.Fatalf("deleted collection: %d", code)
	}
	_, in = callCollections(t, e, http.MethodGet, "/api/collections/containing?provider=YAHOO&symbol=NVDA", "")
	if got := in["collections"].([]any); len(got) != 2 {
		t.Fatalf("NVDA stays in Medicine and the imported copy: %v", got)
	}
}

func TestCollectionModelPortfolioWeights(t *testing.T) {
	e := collectionEngine()
	_, model := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Model","kind":"model"}`)
	base := "/api/collections/" + idOf(model) + "/members"
	if code, body := callCollections(t, e, http.MethodPost, base, `{"instrument":{"provider":"YAHOO","symbol":"SPY"},"weight":70}`); code != http.StatusCreated || body["weight"].(float64) != 70 {
		t.Fatalf("weighted member: %d %v", code, body)
	}
	if code, _ := callCollections(t, e, http.MethodPost, base, `{"instrument":{"provider":"YAHOO","symbol":"TLT"},"weight":40}`); code != http.StatusBadRequest {
		t.Fatalf("over 100%%: %d", code)
	}
	if code, _ := callCollections(t, e, http.MethodPost, base, `{"instrument":{"provider":"YAHOO","symbol":"TLT"}}`); code != http.StatusCreated {
		t.Fatalf("an unweighted member stays unweighted: %d", code)
	}
}

func TestCollectionRoutesRejectBadInput(t *testing.T) {
	e := collectionEngine()
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/collections/0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/collections/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/collections/42", "", http.StatusNotFound},
		{http.MethodPatch, "/api/collections/42", `{"name":"x"}`, http.StatusNotFound},
		{http.MethodPost, "/api/collections/42/members", `{"instrument":{"provider":"YAHOO","symbol":"A"}}`, http.StatusNotFound},
		{http.MethodPost, "/api/collections", `{"name":""}`, http.StatusBadRequest},
		{http.MethodPost, "/api/collections/1/members/1/transfer", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/collections/containing?provider=&symbol=A", "", http.StatusBadRequest},
		{http.MethodPost, "/api/collections/import", `{"format":"x","version":1,"collections":[]}`, http.StatusBadRequest},
	} {
		if code, body := callCollections(t, e, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s: %d (want %d) %v", c.method, c.path, code, c.want, body)
		}
	}
}

func TestCollectionsWithoutStorage(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	if code, _ := callCollections(t, e, http.MethodGet, "/api/collections", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("no database: %d", code)
	}
}

func TestCollectionFromTrackersCopiesWithoutChangingThem(t *testing.T) {
	e := collectionEngine()
	cfg := &SharedMarketConfig{}
	cfg.Add("AAPL", market.ParseProvider("YAHOO"))
	cfg.Add("VOLV-B.ST", market.ParseProvider("YAHOO"))
	e.config = cfg
	code, body := callCollections(t, e, http.MethodPost, "/api/collections/from-trackers", `{}`)
	if code != http.StatusCreated || body["added"].(float64) != 2 {
		t.Fatalf("from trackers: %d %v", code, body)
	}
	c := body["collection"].(map[string]any)
	if c["name"] != "Tracked instruments" || c["member_count"].(float64) != 2 || !strings.Contains(c["description"].(string), "UTC") {
		t.Fatalf("collection = %v", c)
	}
	if len(cfg.Targets()) != 2 {
		t.Fatal("the tracker list must stay as it was")
	}
	if code, _ := callCollections(t, e, http.MethodPost, "/api/collections/from-trackers", `{}`); code != http.StatusConflict {
		t.Fatalf("a second copy under the same name: %d", code)
	}
}
