package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"floweysales/internal/storage"
)

func signedHeaders(secret, delivery string, body []byte, at time.Time) http.Header {
	ts := strconv.FormatInt(at.Unix(), 10)
	h := http.Header{}
	h.Set(adapterTimestampHeader, ts)
	h.Set(adapterDeliveryHeader, delivery)
	h.Set(adapterSignatureHeader, signAdapterDelivery(secret, ts, delivery, body))
	return h
}

func TestAdapterSignatureVerification(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"value":42}`)
	if reason := verifyAdapterDelivery(signedHeaders("s3cret", "d-1", body, now), "s3cret", body, now); reason != "" {
		t.Fatalf("valid delivery refused: %s", reason)
	}
	cases := map[string]struct {
		h    http.Header
		body []byte
		want string
	}{
		"tampered body":   {signedHeaders("s3cret", "d-1", body, now), []byte(`{"value":43}`), "signature mismatch"},
		"wrong secret":    {signedHeaders("other", "d-1", body, now), body, "signature mismatch"},
		"stale timestamp": {signedHeaders("s3cret", "d-1", body, now.Add(-6*time.Minute)), body, "window"},
		"future":          {signedHeaders("s3cret", "d-1", body, now.Add(6*time.Minute)), body, "window"},
		"bad delivery ID": {signedHeaders("s3cret", "has space", body, now), body, "delivery ID"},
		"missing headers": {http.Header{}, body, "missing"},
	}
	for name, c := range cases {
		if got := verifyAdapterDelivery(c.h, "s3cret", c.body, now); !strings.Contains(got, c.want) {
			t.Errorf("%s: reason %q, want %q", name, got, c.want)
		}
	}
	// The delivery ID is signed, so a captured request cannot be replayed under a new ID.
	replayed := signedHeaders("s3cret", "d-1", body, now)
	replayed.Set(adapterDeliveryHeader, "d-2")
	if got := verifyAdapterDelivery(replayed, "s3cret", body, now); got != "signature mismatch" {
		t.Fatalf("re-labelled delivery accepted: %q", got)
	}
}

func TestAdapterEventValueFollowsThePointer(t *testing.T) {
	body := []byte(`{"data":{"price":101.25,"ok":true,"note":"hi","tags":["a/b"],"a~b":{"x/y":"esc"}}}`)
	cases := []struct{ pointer, typ, want string }{
		{"/data/price", "number", "101.25"},
		{"/data/ok", "boolean", "true"},
		{"/data/note", "text", "hi"},
		{"/data/tags/0", "text", "a/b"},
		{"/data/a~0b/x~1y", "text", "esc"},
		{"/data/tags", "text", `["a/b"]`},
	}
	for _, c := range cases {
		if got, err := adapterEventValue(body, c.pointer, c.typ); err != nil || got != c.want {
			t.Errorf("%s: %q %v, want %q", c.pointer, got, err, c.want)
		}
	}
	for _, bad := range []string{"/missing", "/data/tags/1", "/data/tags/01", "/data/price/x"} {
		if _, err := adapterEventValue(body, bad, "text"); err == nil {
			t.Errorf("pointer %s resolved", bad)
		}
	}
	if _, err := adapterEventValue(body, "/data", "number"); err == nil {
		t.Error("an object must not become a number event")
	}
	if _, err := adapterEventValue([]byte(`{"a":1} {"b":2}`), "", "text"); err == nil {
		t.Error("two JSON values accepted")
	}
	if got, err := adapterEventValue([]byte(`7`), "", "number"); err != nil || got != "7" {
		t.Errorf("whole-body number: %q %v", got, err)
	}
}

func TestAdapterRateLimiterRefills(t *testing.T) {
	var l adapterLimiter
	now := time.Unix(0, 0)
	for i := 0; i < 3; i++ {
		if !l.allow(1, 3, now) {
			t.Fatalf("request %d refused within the burst", i)
		}
	}
	if l.allow(1, 3, now) {
		t.Fatal("burst exceeded")
	}
	if !l.allow(2, 3, now) {
		t.Fatal("adapters must not share a bucket")
	}
	if !l.allow(1, 3, now.Add(20*time.Second)) {
		t.Fatal("one token should refill after 20s at 3/min")
	}
}

func TestAdapterRequestValidation(t *testing.T) {
	ok := adapterRequest{Name: " Prices ", PayloadPointer: "/value"}
	if problem := ok.normalize(); problem != "" || ok.Name != "Prices" || ok.RatePerMinute != defaultAdapterRate {
		t.Fatalf("valid request: %q %+v", problem, ok)
	}
	for _, bad := range []adapterRequest{{}, {Name: "x", PayloadPointer: "value"}, {Name: "x", RatePerMinute: maxAdapterRate + 1}, {Name: strings.Repeat("n", 101)}} {
		if bad.normalize() == "" {
			t.Errorf("invalid request accepted: %+v", bad)
		}
	}
	slug, err := newAdapterSlug()
	if err != nil || !adapterSlugPattern.MatchString(slug) {
		t.Fatalf("slug %q %v", slug, err)
	}
}

func TestAdapterSecretIsBoundToItsSlug(t *testing.T) {
	t.Setenv(apiSecretEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	secret, sealed, err := newSealedAdapterSecret("aaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("seal: %q %v", secret, err)
	}
	if opened, err := openAPISecret(adapterSecretKind("aaaaaaaaaaaaaaaaaaaaaaaaaa"), sealed); err != nil || opened != secret {
		t.Fatalf("open: %v", err)
	}
	if _, err := openAPISecret(adapterSecretKind("bbbbbbbbbbbbbbbbbbbbbbbbbb"), sealed); err == nil {
		t.Fatal("a secret copied onto another adapter must not open")
	}
}

// --- MySQL-backed delivery test ---

func TestSignedDeliveryQueuesSubscribersOnce(t *testing.T) {
	env, raw := queueEnv(t)
	t.Setenv(apiSecretEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	ctx := context.Background()
	event := fmt.Sprintf("hook.price.%d", time.Now().UnixNano())
	if _, err := env.db.CreateBlueprintTrigger(ctx, storage.BlueprintTrigger{EventKey: event, PayloadType: "number"}, 1); err != nil {
		t.Fatal(err)
	}
	sub, err := env.db.SaveAgentFlow(ctx, storage.AgentFlow{Name: "Price subscriber", Enabled: true, EventKey: event,
		Graph: json.RawMessage(`{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"up","type":"transform","transform":"suffix","value":"!"}],"edges":[{"from":"t","to":"up"}]}`)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := newAdapterSlug()
	secret, sealed, err := newSealedAdapterSecret(slug)
	if err != nil {
		t.Fatal(err)
	}
	a, err := env.db.CreateFlowEventAdapter(ctx, storage.FlowEventAdapter{Name: "Prices", Slug: slug, EventKey: event, SecretSealed: sealed,
		PayloadPointer: "/data/price", RatePerMinute: 10, Enabled: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	handler := env.engine.Handler()
	deliver := func(body string, h http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/hooks/flow-events/"+slug, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	body := `{"data":{"price":101.5}}`
	h := signedHeaders(secret, "evt-1", []byte(body), time.Now())
	if rec := deliver(body, h); rec.Code != 202 || !strings.Contains(rec.Body.String(), `"queued":1`) {
		t.Fatalf("first delivery: %d %s", rec.Code, rec.Body)
	}
	if rec := deliver(body, h); rec.Code != 200 || !strings.Contains(rec.Body.String(), "duplicate") {
		t.Fatalf("retried delivery: %d %s", rec.Code, rec.Body)
	}
	if rec := deliver(body, signedHeaders("wrong", "evt-2", []byte(body), time.Now())); rec.Code != 401 {
		t.Fatalf("bad signature: %d %s", rec.Code, rec.Body)
	}
	text := `{"data":{"price":"cheap"}}`
	if rec := deliver(text, signedHeaders(secret, "evt-3", []byte(text), time.Now())); rec.Code != 422 {
		t.Fatalf("mistyped value: %d %s", rec.Code, rec.Body)
	}
	if rec := deliver(body, signedHeaders(secret, "evt-4", []byte(body), time.Now())); rec.Code != 202 {
		t.Fatalf("second delivery: %d %s", rec.Code, rec.Body)
	}

	var runs int
	var input, trigger string
	if err := raw.QueryRow("SELECT COUNT(*),MAX(input_text),MAX(trigger_type) FROM agent_flow_runs WHERE flow_id=?", sub.ID).Scan(&runs, &input, &trigger); err != nil {
		t.Fatal(err)
	}
	// evt-1 and evt-4 each queue one run; the retry of evt-1 queues nothing.
	if runs != 2 || input != "101.5" || trigger != "webhook" {
		t.Fatalf("runs %d input %q trigger %q", runs, input, trigger)
	}
	deliveries, err := env.db.ListFlowEventDeliveries(ctx, a.ID)
	if err != nil || len(deliveries) != 4 {
		t.Fatalf("deliveries: %+v %v", deliveries, err)
	}
	got, _ := env.db.GetFlowEventAdapter(ctx, a.ID)
	if got.Accepted != 2 || got.Rejected != 2 {
		t.Fatalf("counters: %+v", got)
	}

	// A disabled adapter looks like an unknown one.
	got.Enabled = false
	if err := env.db.UpdateFlowEventAdapter(ctx, got); err != nil {
		t.Fatal(err)
	}
	if rec := deliver(body, signedHeaders(secret, "evt-5", []byte(body), time.Now())); rec.Code != 404 {
		t.Fatalf("disabled adapter: %d", rec.Code)
	}
}
