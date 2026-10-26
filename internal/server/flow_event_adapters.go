package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"floweysales/internal/storage"
)

// Event adapters are signed inbound webhooks. Each one delivers a single
// typed trigger: a third party POSTs JSON to /api/hooks/flow-events/{slug},
// signed with the adapter's secret, and every enabled flow subscribed to the
// adapter's event key is queued once per delivery ID. The public route never
// uses a session, and a request that does not verify changes nothing but the
// adapter's delivery log.
const (
	adapterSignatureHeader = "X-Flowey-Signature"
	adapterTimestampHeader = "X-Flowey-Timestamp"
	adapterDeliveryHeader  = "X-Flowey-Delivery"
	// adapterClockSkew bounds how old or early a signed timestamp may be.
	adapterClockSkew   = 5 * time.Minute
	maxAdapterBody     = 16 << 10
	maxAdapterRate     = 600
	defaultAdapterRate = 30
	// Deliveries are kept long after adapterClockSkew, so a captured request
	// cannot be replayed under the same delivery ID either.
	adapterDeliveryRetention = 30 * 24 * time.Hour
)

var (
	adapterSlugPattern     = regexp.MustCompile(`^[a-z2-7]{26}$`)
	adapterDeliveryPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// adapterSecretKind binds a sealed secret to its adapter's fixed slug, so a
// ciphertext copied onto another adapter does not open.
func adapterSecretKind(slug string) string { return "flow_event_adapter:" + slug }

func newAdapterSlug() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(base32NoPad.EncodeToString(b))[:26], nil
}

func newAdapterSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// signAdapterDelivery is the signature senders compute: hex HMAC-SHA256 of
// "<timestamp>.<delivery id>.<body>" keyed by the adapter secret.
func signAdapterDelivery(secret, timestamp, delivery string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + delivery + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// verifyAdapterDelivery checks the signed headers and returns the reason a
// delivery is refused, or "" when it verifies.
func verifyAdapterDelivery(h http.Header, secret string, body []byte, now time.Time) string {
	ts, delivery, sig := h.Get(adapterTimestampHeader), h.Get(adapterDeliveryHeader), h.Get(adapterSignatureHeader)
	if ts == "" || delivery == "" || sig == "" {
		return "missing signature, timestamp or delivery header"
	}
	if !adapterDeliveryPattern.MatchString(delivery) {
		return "invalid delivery ID"
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "invalid timestamp"
	}
	if skew := now.Sub(time.Unix(unix, 0)); skew > adapterClockSkew || skew < -adapterClockSkew {
		return "timestamp outside the allowed window"
	}
	if !hmac.Equal([]byte(sig), []byte(signAdapterDelivery(secret, ts, delivery, body))) {
		return "signature mismatch"
	}
	return ""
}

// adapterEventValue selects the event value from the body with an RFC 6901
// pointer and renders it as the raw text eventPayload type-checks.
func adapterEventValue(body []byte, pointer, payloadType string) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return "", errors.New("body is not JSON")
	}
	if dec.More() {
		return "", errors.New("body must be a single JSON value")
	}
	v, err := resolveJSONPointer(doc, pointer)
	if err != nil {
		return "", err
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case bool:
		return strconv.FormatBool(x), nil
	case nil:
		return "", errors.New("event value is null")
	}
	if payloadType != "text" {
		return "", fmt.Errorf("event value must be a %s", payloadType)
	}
	raw, err := json.Marshal(v)
	return string(raw), err
}

func resolveJSONPointer(doc any, pointer string) (any, error) {
	if pointer == "" {
		return doc, nil
	}
	cur := doc
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, fmt.Errorf("payload pointer %s not found", pointer)
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(node) || (len(token) > 1 && token[0] == '0') {
				return nil, fmt.Errorf("payload pointer %s not found", pointer)
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("payload pointer %s not found", pointer)
		}
	}
	return cur, nil
}

func validAdapterPointer(p string) bool {
	return p == "" || (strings.HasPrefix(p, "/") && len(p) <= 200 && utf8.ValidString(p))
}

// adapterLimiter is a per-adapter token bucket refilled at rate per minute.
// It is counted per instance, before the signature is checked, so floods of
// unsigned requests cannot write to the delivery log either.
type adapterLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*adapterBucket
}

type adapterBucket struct {
	tokens float64
	last   time.Time
}

func (l *adapterLimiter) allow(id int64, perMinute int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[int64]*adapterBucket{}
	}
	b, ok := l.buckets[id]
	if !ok {
		b = &adapterBucket{tokens: float64(perMinute), last: now}
		l.buckets[id] = b
	}
	b.tokens = min(float64(perMinute), b.tokens+now.Sub(b.last).Minutes()*float64(perMinute))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type adapterRequest struct {
	Name           string `json:"name"`
	EventKey       string `json:"event_key"`
	PayloadPointer string `json:"payload_pointer"`
	RatePerMinute  int    `json:"rate_per_minute"`
	Enabled        *bool  `json:"enabled"`
}

func (req *adapterRequest) normalize() string {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 100 {
		return "Name is required (at most 100 characters)"
	}
	if !validAdapterPointer(req.PayloadPointer) {
		return "Payload pointer must be empty or a JSON pointer such as /data/value"
	}
	if req.RatePerMinute == 0 {
		req.RatePerMinute = defaultAdapterRate
	}
	if req.RatePerMinute < 1 || req.RatePerMinute > maxAdapterRate {
		return fmt.Sprintf("Rate must be between 1 and %d deliveries per minute", maxAdapterRate)
	}
	return ""
}

func decodeAdapterRequest(w http.ResponseWriter, r *http.Request) (adapterRequest, bool) {
	var req adapterRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid adapter JSON"})
		return req, false
	}
	if problem := req.normalize(); problem != "" {
		writeJSON(w, 400, map[string]string{"error": problem})
		return req, false
	}
	return req, true
}

func adapterID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "Invalid adapter id"})
		return 0, false
	}
	return id, true
}

// adapterView is what the admin API returns: the adapter and the path to
// deliver to. The secret is only included right after it is generated.
func adapterView(a storage.FlowEventAdapter, secret string) map[string]any {
	out := map[string]any{"id": a.ID, "name": a.Name, "enabled": a.Enabled, "adapter": a,
		"delivery_path": "/api/hooks/flow-events/" + a.Slug}
	if secret != "" {
		out["secret"] = secret
	}
	return out
}

func (e *Engine) registerFlowEventAdapterRoutes() {
	e.mux.HandleFunc("GET /api/admin/flow-event-adapters", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		adapters, err := e.db.ListFlowEventAdapters(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Adapter storage unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"adapters": adapters, "signature_header": adapterSignatureHeader,
			"timestamp_header": adapterTimestampHeader, "delivery_header": adapterDeliveryHeader})
	})
	e.mux.HandleFunc("POST /api/admin/flow-event-adapters", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		req, ok := decodeAdapterRequest(w, r)
		if !ok {
			return
		}
		if _, err := e.db.GetBlueprintTrigger(r.Context(), req.EventKey); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Create the event trigger first"})
			return
		}
		if _, err := apiSecretCipher(); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Set " + apiSecretEnv + " before creating event adapters"})
			return
		}
		slug, err := newAdapterSlug()
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not generate the adapter URL"})
			return
		}
		secret, sealed, err := newSealedAdapterSecret(slug)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not seal the adapter secret"})
			return
		}
		user, _ := e.currentUser(r)
		a, err := e.db.CreateFlowEventAdapter(r.Context(), storage.FlowEventAdapter{Name: req.Name, Slug: slug, EventKey: req.EventKey,
			SecretSealed: sealed, PayloadPointer: req.PayloadPointer, RatePerMinute: req.RatePerMinute, Enabled: req.Enabled == nil || *req.Enabled}, user.ID)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not save the adapter"})
			return
		}
		writeJSON(w, 201, adapterView(a, secret))
	})
	e.mux.HandleFunc("PUT /api/admin/flow-event-adapters/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, ok := adapterID(w, r)
		if !ok {
			return
		}
		req, ok := decodeAdapterRequest(w, r)
		if !ok {
			return
		}
		a, err := e.db.GetFlowEventAdapter(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Adapter not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Adapter storage unavailable"})
			return
		}
		if req.EventKey != "" && req.EventKey != a.EventKey {
			writeJSON(w, 400, map[string]string{"error": "An adapter's event key cannot change; create a new adapter"})
			return
		}
		a.Name, a.PayloadPointer, a.RatePerMinute = req.Name, req.PayloadPointer, req.RatePerMinute
		if req.Enabled != nil {
			a.Enabled = *req.Enabled
		}
		if err := e.db.UpdateFlowEventAdapter(r.Context(), a); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not save the adapter"})
			return
		}
		writeJSON(w, 200, adapterView(a, ""))
	})
	e.mux.HandleFunc("POST /api/admin/flow-event-adapters/{id}/rotate-secret", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, ok := adapterID(w, r)
		if !ok {
			return
		}
		a, err := e.db.GetFlowEventAdapter(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Adapter not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Adapter storage unavailable"})
			return
		}
		secret, sealed, err := newSealedAdapterSecret(a.Slug)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not seal the adapter secret"})
			return
		}
		if err := e.db.RotateFlowEventAdapterSecret(r.Context(), id, sealed); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not save the adapter secret"})
			return
		}
		writeJSON(w, 200, adapterView(a, secret))
	})
	e.mux.HandleFunc("DELETE /api/admin/flow-event-adapters/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, ok := adapterID(w, r)
		if !ok {
			return
		}
		err := e.db.DeleteFlowEventAdapter(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Adapter not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not delete the adapter"})
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": true, "id": id})
	})
	e.mux.HandleFunc("GET /api/admin/flow-event-adapters/{id}/deliveries", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, ok := adapterID(w, r)
		if !ok {
			return
		}
		deliveries, err := e.db.ListFlowEventDeliveries(r.Context(), id)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Adapter storage unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"deliveries": deliveries})
	})
	e.mux.HandleFunc("POST /api/hooks/flow-events/{slug}", e.receiveAdapterDelivery)
}

// newSealedAdapterSecret generates a secret and seals it for the adapter.
func newSealedAdapterSecret(slug string) (secret string, sealed []byte, err error) {
	if secret, err = newAdapterSecret(); err != nil {
		return "", nil, err
	}
	sealed, err = sealAPISecret(adapterSecretKind(slug), secret)
	return secret, sealed, err
}

// receiveAdapterDelivery is the public, session-free webhook endpoint.
func (e *Engine) receiveAdapterDelivery(w http.ResponseWriter, r *http.Request) {
	notFound := func() { writeJSON(w, 404, map[string]string{"error": "Not found"}) }
	slug := r.PathValue("slug")
	if !adapterSlugPattern.MatchString(slug) || e.db == nil {
		notFound()
		return
	}
	a, err := e.db.GetFlowEventAdapterBySlug(r.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !a.Enabled) {
		notFound()
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "Temporarily unavailable"})
		return
	}
	if !e.adapterLimits.allow(a.ID, a.RatePerMinute, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 429, map[string]string{"error": "Rate limit exceeded"})
		return
	}
	reject := func(status int, reason string) {
		e.recordDelivery(r.Context(), a.ID, storage.FlowEventDelivery{Status: storage.DeliveryRejected, Reason: reason})
		writeJSON(w, status, map[string]string{"error": reason})
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAdapterBody))
	if err != nil {
		reject(413, fmt.Sprintf("body exceeds %d bytes", maxAdapterBody))
		return
	}
	secret, err := openAPISecret(adapterSecretKind(a.Slug), a.SecretSealed)
	if err != nil {
		log.Printf("[Flows] Adapter %d secret unavailable: %v", a.ID, err)
		writeJSON(w, 503, map[string]string{"error": "Temporarily unavailable"})
		return
	}
	if reason := verifyAdapterDelivery(r.Header, secret, body, time.Now()); reason != "" {
		reject(401, reason)
		return
	}
	digest := sha256.Sum256(body)
	delivery := storage.FlowEventDelivery{Status: storage.DeliveryAccepted, DeliveryID: r.Header.Get(adapterDeliveryHeader), PayloadSHA256: hex.EncodeToString(digest[:])}
	def, err := e.db.GetBlueprintTrigger(r.Context(), a.EventKey)
	if err != nil {
		reject(422, "event "+a.EventKey+" is no longer defined")
		return
	}
	raw, err := adapterEventValue(body, a.PayloadPointer, def.PayloadType)
	if err == nil {
		raw, err = eventPayload(def, raw)
	}
	if err != nil {
		reject(422, err.Error())
		return
	}
	queued, err := e.queueAdapterDelivery(r.Context(), a, delivery.DeliveryID, raw)
	if errors.Is(err, storage.ErrFlowQueueFull) {
		w.Header().Set("Retry-After", "60")
		reject(503, "the run queue is full")
		return
	}
	if err != nil {
		log.Printf("[Flows] Adapter %d delivery %s not queued: %v", a.ID, delivery.DeliveryID, err)
		writeJSON(w, 503, map[string]string{"error": "Temporarily unavailable"})
		return
	}
	delivery.QueuedRuns = queued
	_, err = e.db.RecordFlowEventDelivery(r.Context(), a.ID, delivery)
	if errors.Is(err, storage.ErrDuplicateDelivery) {
		writeJSON(w, 200, map[string]any{"status": "duplicate", "delivery_id": delivery.DeliveryID})
		return
	}
	if err != nil {
		// The runs are queued; a retry finds them by emit key and queues nothing twice.
		log.Printf("[Flows] Adapter %d delivery %s not logged: %v", a.ID, delivery.DeliveryID, err)
	}
	writeJSON(w, 202, map[string]any{"status": "accepted", "delivery_id": delivery.DeliveryID, "queued": queued})
}

// queueAdapterDelivery queues every enabled subscriber of the adapter's event.
// Runs are keyed by adapter, delivery and flow, so a retried delivery whose
// first attempt failed after queueing cannot start a subscriber twice.
func (e *Engine) queueAdapterDelivery(ctx context.Context, a storage.FlowEventAdapter, deliveryID, input string) (int, error) {
	subscribers, err := e.db.FindEventAgentFlows(ctx, a.EventKey)
	if err != nil {
		return 0, err
	}
	if len(subscribers) > maxEventFanout {
		return 0, fmt.Errorf("event %s has more than %d subscribed flows", a.EventKey, maxEventFanout)
	}
	deliveryHash := sha256.Sum256([]byte(deliveryID))
	runs := []storage.NewFlowRun{}
	for _, sub := range subscribers {
		target, revision, err := e.db.ResolveAgentFlow(ctx, sub.ID, 0)
		if err != nil {
			return 0, fmt.Errorf("subscribed flow %d unavailable: %w", sub.ID, err)
		}
		runs = append(runs, storage.NewFlowRun{FlowID: target.ID, Trigger: "webhook", Input: input, Revision: revision, Graph: target.Graph,
			EmitKey: fmt.Sprintf("hook:%d:%x:%d", a.ID, deliveryHash[:12], target.ID)})
	}
	if len(runs) == 0 {
		return 0, nil
	}
	queued, _, err := e.db.EnqueueEmittedAgentFlowRuns(ctx, runs)
	if err == nil {
		e.wakeFlowWorkers()
	}
	return queued, err
}

func (e *Engine) recordDelivery(ctx context.Context, adapterID int64, del storage.FlowEventDelivery) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if _, err := e.db.RecordFlowEventDelivery(ctx, adapterID, del); err != nil {
		log.Printf("[Flows] Adapter %d delivery not logged: %v", adapterID, err)
	}
}
