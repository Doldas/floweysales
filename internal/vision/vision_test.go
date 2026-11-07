package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseNumber(t *testing.T) {
	good := []struct {
		in  string
		v   float64
		neg bool
	}{
		{"1,234.5", 1234.5, false}, {"1 234,5", 1234.5, false}, {"1.234,5", 1234.5, false},
		{"(4 300)", -4300, true}, {"-12,5", -12.5, true}, {"−12.5", -12.5, true},
		{"1,234,567", 1234567, false}, {"1.234.567", 1234567, false}, {"12.5 %", 12.5, false},
		{"4 300", 4300, false}, {"0", 0, false}, {"7,5", 7.5, false}, {"123-", -123, true},
	}
	for _, c := range good {
		v, neg, err := ParseNumber(c.in)
		if err != nil || v != c.v || neg != c.neg {
			t.Errorf("%q: got %v %v %v, want %v %v", c.in, v, neg, err, c.v, c.neg)
		}
	}
	// Ambiguous or not numbers are refused, never guessed.
	for _, bad := range []string{"1,234", "1.234", "12,34,567", "abc", "", "4.3 MSEK", "1.2.3,4,5"} {
		if _, _, err := ParseNumber(bad); err == nil {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestValidateKeepsPrintedValueAndDerivesSign(t *testing.T) {
	o := Observation{Kind: "table_cell", Label: "Net result", OriginalValue: "(1 234,5)", SourceExcerpt: "Net result (1 234,5) 2 100",
		Period: "Q3 2024", PeriodKind: "quarter", Currency: "sek", Unit: "MSEK", Uncertainty: "low"}
	Validate(&o)
	if o.Value == nil || *o.Value != -1234.5 || o.Sign != "negative" || !o.Verified || o.OriginalValue != "(1 234,5)" || o.Currency != "SEK" {
		t.Fatalf("%+v", o)
	}
}

func TestValidateFlagsUnverifiedCases(t *testing.T) {
	tests := []struct {
		name string
		o    Observation
		flag string
	}{
		{"annual labelled quarter", Observation{OriginalValue: "100", SourceExcerpt: "Revenue 100", Period: "Full year 2023", PeriodKind: "quarter"}, "annual_figure_labelled_quarter"},
		{"year only labelled quarter", Observation{OriginalValue: "100", SourceExcerpt: "Revenue 100", Period: "2023", PeriodKind: "quarter"}, "annual_figure_labelled_quarter"},
		{"quarter labelled annual", Observation{OriginalValue: "100", SourceExcerpt: "Revenue 100", Period: "Q2 2024", PeriodKind: "annual"}, "quarter_labelled_annual"},
		{"value not in excerpt", Observation{OriginalValue: "100", SourceExcerpt: "Revenue 101", Period: "Q2 2024", PeriodKind: "quarter"}, "value_not_in_excerpt"},
		{"missing period", Observation{OriginalValue: "100", SourceExcerpt: "Revenue 100"}, "period_missing"},
		{"ambiguous", Observation{OriginalValue: "1,234", SourceExcerpt: "1,234", Period: "Q1"}, "ambiguous_separator"},
		{"uncertain", Observation{OriginalValue: "5", SourceExcerpt: "5", Period: "Q1 2024", PeriodKind: "quarter", Uncertainty: "high"}, "model_uncertain"},
	}
	for _, c := range tests {
		Validate(&c.o)
		if c.o.Verified || !contains(c.o.Flags, c.flag) {
			t.Errorf("%s: verified=%v flags=%v", c.name, c.o.Verified, c.o.Flags)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestCheckTotals(t *testing.T) {
	mk := func(role string, val string) Observation {
		return Observation{Group: "Income", Role: role, OriginalValue: val, SourceExcerpt: val, Period: "Q3 2024", PeriodKind: "quarter", Unit: "MSEK", Uncertainty: "low"}
	}
	ok := []Observation{mk("component", "100"), mk("component", "50"), mk("total", "150")}
	bad := []Observation{mk("component", "100"), mk("component", "50"), mk("total", "170")}
	for i := range ok {
		Validate(&ok[i])
	}
	for i := range bad {
		Validate(&bad[i])
	}
	CheckTotals(ok)
	CheckTotals(bad)
	for _, o := range ok {
		if !o.Verified || !contains(o.Flags, "total_consistent") {
			t.Errorf("consistent group: %+v", o)
		}
	}
	for _, o := range bad {
		if o.Verified || !contains(o.Flags, "total_mismatch") {
			t.Errorf("mismatched group: %+v", o)
		}
	}
	// A lone component is never "checked".
	one := []Observation{mk("component", "100"), mk("total", "100")}
	for i := range one {
		Validate(&one[i])
	}
	CheckTotals(one)
	if contains(one[0].Flags, "total_consistent") {
		t.Error("a total over a single component is not a check")
	}
}

func TestParseReplyForcesPageAndToleratesFences(t *testing.T) {
	reply := "Here you go:\n```json\n{\"observations\":[{\"kind\":\"table_cell\",\"page\":99,\"label\":\"Revenue\",\"original_value\":\"4 300\",\"source_excerpt\":\"Revenue 4 300\",\"period\":\"Q3 2024\",\"period_kind\":\"quarter\",\"uncertainty\":\"low\",\"bbox\":[0.1,0.2,0.5,0.3]},{\"kind\":\"x\",\"original_value\":\"9\",\"bbox\":[2,0,1,1]}],\"limitations\":\"footnotes unreadable\"}\n```"
	obs, lim, err := ParseReply(reply, 7)
	if err != nil || len(obs) != 2 || obs[0].Page != 7 || obs[1].Page != 7 || lim != "footnotes unreadable" {
		t.Fatalf("%+v %q %v", obs, lim, err)
	}
	if obs[0].Value == nil || *obs[0].Value != 4300 || !obs[0].Verified || obs[0].BBox == nil {
		t.Errorf("%+v", obs[0])
	}
	if obs[1].BBox != nil || obs[1].Kind != "text" || obs[1].Verified {
		t.Errorf("%+v", obs[1])
	}
	if _, _, err := ParseReply("sorry, I cannot", 1); err == nil {
		t.Error("prose must be an error, not an empty success")
	}
}

// fakeModel is an OpenAI-compatible endpoint that sees images or not.
func fakeModel(t *testing.T, handler func(model string, parts []map[string]any) (int, string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "bad", 401)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &req); err != nil || req.Stream {
			http.Error(w, "bad", 400)
			return
		}
		var parts []map[string]any
		for _, m := range req.Messages {
			if arr, ok := m.Content.([]any); ok {
				for _, p := range arr {
					parts = append(parts, p.(map[string]any))
				}
			}
		}
		code, body := handler(req.Model, parts)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
}

func answer(text string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}}},
		"usage":   map[string]int{"prompt_tokens": 800, "completion_tokens": 100},
	})
	return string(b)
}

func hasImage(parts []map[string]any) bool {
	for _, p := range parts {
		if p["type"] == "image_url" {
			u := p["image_url"].(map[string]any)["url"].(string)
			return strings.HasPrefix(u, "data:image/") && strings.Contains(u, ";base64,")
		}
	}
	return false
}

func TestProbeAcceptsAVisionModel(t *testing.T) {
	srv := fakeModel(t, func(model string, parts []map[string]any) (int, string) {
		if !hasImage(parts) {
			return 400, `{"error":{"message":"no image"}}`
		}
		return 200, answer("Red")
	})
	defer srv.Close()
	res := (&Client{BaseURL: srv.URL, APIKey: "sk-test", Model: "vis"}).Probe(context.Background())
	if !res.OK || res.Code != "ok" {
		t.Fatalf("%+v", res)
	}
}

func TestProbeRejectsATextOnlyModel(t *testing.T) {
	// An endpoint that refuses image content.
	refuse := fakeModel(t, func(string, []map[string]any) (int, string) {
		return 400, `{"error":{"message":"messages[0].content must be a string; image input is not supported"}}`
	})
	defer refuse.Close()
	res := (&Client{BaseURL: refuse.URL, APIKey: "sk-test", Model: "text"}).Probe(context.Background())
	if res.OK || res.Code != CodeImageRejected {
		t.Fatalf("%+v", res)
	}
	// A lenient endpoint whose model ignores the image and answers anyway.
	blind := fakeModel(t, func(string, []map[string]any) (int, string) { return 200, answer("I cannot see an image.") })
	defer blind.Close()
	res = (&Client{BaseURL: blind.URL, APIKey: "sk-test", Model: "text"}).Probe(context.Background())
	if res.OK || res.Code != "wrong_answer" {
		t.Fatalf("%+v", res)
	}
	// Wrong key and unreachable endpoint.
	res = (&Client{BaseURL: blind.URL, APIKey: "nope", Model: "m"}).Probe(context.Background())
	if res.OK || res.Code != CodeAuth || strings.Contains(res.Message, "nope") {
		t.Fatalf("%+v", res)
	}
	res = (&Client{BaseURL: "http://127.0.0.1:1", Model: "m"}).Probe(context.Background())
	if res.OK || res.Code != CodeUnreachable {
		t.Fatalf("%+v", res)
	}
}

func TestDescribeFallsBackWhenTheModelIsMissing(t *testing.T) {
	var seen []string
	srv := fakeModel(t, func(model string, parts []map[string]any) (int, string) {
		seen = append(seen, model)
		if model == "gone" {
			return 404, `{"error":{"message":"model not found"}}`
		}
		return 200, answer("red")
	})
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Model: "gone", FallbackModel: "backup"}
	rep, err := c.Describe(context.Background(), "", "x", []Image{fixtureImage()})
	if err != nil || rep.Model != "backup" || len(seen) != 2 {
		t.Fatalf("%+v %v %v", rep, err, seen)
	}
}

func TestExtractPageUsesBudgetAndReportsFailuresExplicitly(t *testing.T) {
	var calls int32
	srv := fakeModel(t, func(model string, parts []map[string]any) (int, string) {
		atomic.AddInt32(&calls, 1)
		return 200, answer(`{"observations":[{"kind":"table_cell","label":"Revenue","original_value":"4 300","source_excerpt":"Revenue 4 300","period":"Q3 2024","period_kind":"quarter","uncertainty":"low"}]}`)
	})
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Model: "vis"}
	b := NewBudget(Limits{MaxImagesPerRun: 1})
	p := ExtractPage(context.Background(), c, b, Image{Mime: "image/png", Data: fixtureImage().Data, Page: 3})
	if p.Error != "" || len(p.Observations) != 1 || p.Observations[0].Page != 3 {
		t.Fatalf("%+v", p)
	}
	if s := b.Snapshot(); s.Images != 1 || s.Tokens != 900 || s.Calls != 1 {
		t.Fatalf("%+v", s)
	}
	// The budget is used up: explicit failure, and no further paid call.
	p = ExtractPage(context.Background(), c, b, Image{Mime: "image/png", Data: fixtureImage().Data, Page: 4})
	if p.ErrorCode != CodeBudget || len(p.Observations) != 0 || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("%+v calls=%d", p, calls)
	}
	// A model that does not return JSON is a failure, not "no figures".
	bad := fakeModel(t, func(string, []map[string]any) (int, string) { return 200, answer("It is a chart.") })
	defer bad.Close()
	p = ExtractPage(context.Background(), &Client{BaseURL: bad.URL, APIKey: "sk-test", Model: "v"}, nil, Image{Mime: "image/png", Data: []byte{1}, Page: 1})
	if p.Error == "" || p.ErrorCode != CodeBadReply {
		t.Fatalf("%+v", p)
	}
}

func TestExtractPageHonorsCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := fakeModel(t, func(string, []map[string]any) (int, string) { <-release; return 200, answer("{}") })
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	p := ExtractPage(ctx, &Client{BaseURL: srv.URL, APIKey: "sk-test", Model: "v"}, nil, Image{Mime: "image/png", Data: []byte{1}, Page: 1})
	if p.ErrorCode != CodeTimeout {
		t.Fatalf("%+v", p)
	}
}

func TestBudgetCaps(t *testing.T) {
	b := NewBudget(Limits{MaxImagesPerRun: 3, MaxTokensPerRun: 1000})
	if got := b.Reserve(5); got != 3 {
		t.Fatalf("granted %d", got)
	}
	b.Release(3)
	b.Record(&Usage{PromptTokens: 900, CompletionTokens: 200}, true)
	if got := b.Reserve(1); got != 0 {
		t.Fatalf("token cap must stop further images, granted %d", got)
	}
	b.Record(nil, true)
	if !b.Snapshot().TokensUnknown {
		t.Error("missing usage must be reported as unknown")
	}
}

func TestCacheScopesByUserModelAndSchema(t *testing.T) {
	c := NewCache(2, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }
	img := []byte("page image")
	c.Put(Key("u1", "m", img), Page{Page: 1, Observations: []Observation{{Label: "a"}}})
	if _, ok := c.Get(Key("u1", "m", img)); !ok {
		t.Fatal("hit expected")
	}
	for _, k := range []string{Key("u2", "m", img), Key("u1", "m2", img), Key("u1", "m", []byte("other"))} {
		if _, ok := c.Get(k); ok {
			t.Errorf("must not serve %s", k)
		}
	}
	c.Put("failed", Page{Error: "x"})
	if c.Len() != 1 {
		t.Error("failures are not cached")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get(Key("u1", "m", img)); ok {
		t.Error("entry should expire")
	}
	for i := 0; i < 5; i++ {
		c.Put(fmt.Sprint(i), Page{})
	}
	if c.Len() > 2 {
		t.Errorf("size %d", c.Len())
	}
}

func TestConfigStatusAndValidation(t *testing.T) {
	c, err := Config{BaseURL: "https://api.example.com/v1/", Model: "vis-1", SupportsImages: true, Enabled: true}.Validate()
	if err != nil || c.BaseURL != "https://api.example.com/v1" || c.Limits.MaxPagesPerDocument != 6 {
		t.Fatalf("%+v %v", c, err)
	}
	if c.Usable() || c.Status() != "needs_test" {
		t.Fatalf("unverified config must not be used: %s", c.Status())
	}
	c.VerifiedFingerprint = c.Fingerprint()
	if !c.Usable() || c.Status() != "ready" {
		t.Fatalf("%s", c.Status())
	}
	c.Model = "vis-2"
	if c.Usable() {
		t.Error("changing the model must invalidate the probe")
	}
	for _, bad := range []Config{
		{BaseURL: "ftp://x", Model: "m"}, {BaseURL: "https://u:p@x", Model: "m"},
		{Enabled: true, BaseURL: "https://x", Model: "m"}, // images not declared
		{Enabled: true, SupportsImages: true},             // no endpoint
		{BaseURL: "https://x", Limits: Limits{MaxPagesPerDocument: 999}},
		{BaseURL: "https://x", APIKey: "a\nb"},
	} {
		if _, err := bad.Validate(); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}

func TestBudgetRejectsInvalidReservationAndUnknownUsage(t *testing.T) {
	b := NewBudget(Limits{})
	if n := b.Reserve(-5); n != 0 {
		t.Fatalf("negative reservation: %d", n)
	}
	before := b.Snapshot()
	b.Release(-4)
	if b.Snapshot() != before {
		t.Fatal("negative release changed budget")
	}
	b.Record(nil, true)
	if n := b.Reserve(1); n != 0 {
		t.Fatalf("unknown billing must stop further image calls, got %d", n)
	}
}
