package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"floweysales/internal/storage"
	"floweysales/internal/trading"
)

func TestPaperRequestDigestIgnoresReviewContext(t *testing.T) {
	order := map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 1.0}
	proposal := map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 1.0,
		"client_order_id": "k1", "rationale": "momentum thesis", "decision_source": "flow-run:42"}
	a, err := paperRequestDigest(order)
	if err != nil {
		t.Fatal(err)
	}
	b, err := paperRequestDigest(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("rationale and decision source must not change the order request hash")
	}
	proposal["quantity"] = 2.0
	if c, _ := paperRequestDigest(proposal); c == a {
		t.Fatal("changing an order parameter must change the request hash")
	}
}

func TestPaperDecisionContextValidation(t *testing.T) {
	if _, _, err := paperDecisionContext(map[string]any{}); err == nil {
		t.Fatal("missing rationale must be rejected")
	}
	if _, _, err := paperDecisionContext(map[string]any{"rationale": "   "}); err == nil {
		t.Fatal("blank rationale must be rejected")
	}
	if _, _, err := paperDecisionContext(map[string]any{"rationale": strings.Repeat("x", maxApprovalRationale+1)}); err == nil {
		t.Fatal("oversized rationale must be rejected")
	}
	for _, bad := range []any{"", "has space", 42, strings.Repeat("s", 129)} {
		if _, _, err := paperDecisionContext(map[string]any{"rationale": "ok", "decision_source": bad}); err == nil {
			t.Fatalf("invalid decision_source accepted: %v", bad)
		}
	}
	rationale, source, err := paperDecisionContext(map[string]any{"rationale": "  thesis  "})
	if err != nil || rationale != "thesis" || source != "manual" {
		t.Fatalf("defaults: %q %q %v", rationale, source, err)
	}
	_, source, err = paperDecisionContext(map[string]any{"rationale": "thesis", "decision_source": "flow-run:42"})
	if err != nil || source != "flow-run:42" {
		t.Fatalf("explicit source: %q %v", source, err)
	}
}

func TestPaperApprovalStatusPrecedence(t *testing.T) {
	id := int64(7)
	cases := []struct {
		name string
		in   storage.PaperApproval
		want string
	}{
		{"pending", storage.PaperApproval{}, storage.PaperApprovalPending},
		{"approved", storage.PaperApproval{ApprovedBy: &id}, storage.PaperApprovalApproved},
		{"expired pending", storage.PaperApproval{Expired: true}, storage.PaperApprovalExpired},
		{"expired approval", storage.PaperApproval{ApprovedBy: &id, Expired: true}, storage.PaperApprovalExpired},
		{"rejection beats expiry", storage.PaperApproval{RejectedBy: &id, Expired: true}, storage.PaperApprovalRejected},
		{"consumption is final", storage.PaperApproval{ApprovedBy: &id, ConsumedOrderID: &id, Expired: true}, storage.PaperApprovalConsumed},
	}
	for _, c := range cases {
		if got := storage.PaperApprovalStatus(c.in); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	for _, ok := range []string{"pending", "APPROVED", "rejected", "consumed", "expired", "all"} {
		if !storage.ValidPaperApprovalFilter(ok) {
			t.Errorf("filter %q should be valid", ok)
		}
	}
	if storage.ValidPaperApprovalFilter("open; DROP TABLE") {
		t.Error("unknown filter accepted")
	}
}

func TestApprovalTTLBounds(t *testing.T) {
	if got := trading.DefaultConfig().ApprovalTTLSeconds(); got != trading.DefaultApprovalTTLMinutes*60 {
		t.Fatalf("default ttl: %d", got)
	}
	for _, minutes := range []int{0, -5, trading.MaxApprovalTTLMinutes + 1} {
		if got := (trading.Config{ApprovalTTLMinutes: minutes}).ApprovalTTLSeconds(); got != trading.DefaultApprovalTTLMinutes*60 {
			t.Errorf("ttl %d minutes fell back to %d seconds", minutes, got)
		}
	}
	if got := (trading.Config{ApprovalTTLMinutes: 15}).ApprovalTTLSeconds(); got != 900 {
		t.Fatalf("configured ttl: %d", got)
	}
}

// approvalFixture proposes one paper order under a key unique to this run.
func approvalFixture(t *testing.T, env *testEnv, name string, requester int64) (storage.Order, string, string) {
	t.Helper()
	payload := map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 1,
		"client_order_id": fmt.Sprintf("%s-%d", name, time.Now().UnixNano())}
	key, digest, err := paperApprovalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	err = env.db.RequestPaperApproval(context.Background(), storage.PaperApprovalProposal{
		Key: key, Digest: digest, Payload: `{"symbol":"GIB"}`, Rationale: "review test",
		DecisionSource: "manual", RequestedBy: requester, TTLSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	order := storage.Order{Symbol: "GIB", Side: "BUY", Type: "MARKET", TIF: "GTC", Quantity: 1, Price: 100,
		Status: "FILLED", FillPrice: 100, Timestamp: time.Now().Unix()}
	return order, key, digest
}

func TestPaperApprovalRequiresDistinctApprover(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	order, key, digest := approvalFixture(t, env, "four-eyes", 12)
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 12); err != nil || ok {
		t.Fatalf("requester approved own proposal: %v %v", ok, err)
	}
	if _, _, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest); err == nil {
		t.Fatal("self-approval attempt must not allow an order")
	}
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 13); err != nil || !ok {
		t.Fatalf("second operator could not approve: %v %v", ok, err)
	}
	a, found, err := env.db.GetPaperApproval(ctx, key)
	if err != nil || !found || a.Status != storage.PaperApprovalApproved || a.ApprovedAt == "" ||
		a.Rationale != "review test" || a.DecisionSource != "manual" || a.ExpiresAt == "" {
		t.Fatalf("approval record incomplete: %+v %v", a, err)
	}
}

func TestRejectedOrRevokedApprovalCannotBeUsed(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	order, key, digest := approvalFixture(t, env, "reject-pending", 12)
	if ok, err := env.db.RejectPaperOrder(ctx, key, 13, "thesis no longer valid"); err != nil || !ok {
		t.Fatalf("reject: %v %v", ok, err)
	}
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 13); err != nil || ok {
		t.Fatalf("rejected proposal was approved: %v %v", ok, err)
	}
	if ok, err := env.db.RejectPaperOrder(ctx, key, 13, "again"); err != nil || ok {
		t.Fatalf("rejection must be terminal: %v %v", ok, err)
	}

	order, key, digest = approvalFixture(t, env, "revoke-approved", 12)
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 13); err != nil || !ok {
		t.Fatalf("approve: %v %v", ok, err)
	}
	if ok, err := env.db.RejectPaperOrder(ctx, key, 12, "requester withdrew"); err != nil || !ok {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if _, _, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest); err == nil {
		t.Fatal("revoked approval allowed an order")
	}
	a, _, err := env.db.GetPaperApproval(ctx, key)
	if err != nil || a.Status != storage.PaperApprovalRejected || a.RejectionReason != "requester withdrew" ||
		a.RejectedBy == nil || *a.RejectedBy != 12 || a.ConsumedOrderID != nil {
		t.Fatalf("revocation audit incorrect: %+v %v", a, err)
	}
	rejected, err := env.db.ListPaperApprovals(ctx, storage.PaperApprovalRejected, 100)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, item := range rejected {
		listed = listed || item.ClientOrderID == key
	}
	if !listed {
		t.Fatal("revoked request missing from rejected listing")
	}

	order, key, digest = approvalFixture(t, env, "reject-consumed", 12)
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 13); err != nil || !ok {
		t.Fatalf("approve: %v %v", ok, err)
	}
	created, _, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := env.db.RejectPaperOrder(ctx, key, 13, "too late"); err != nil || ok {
		t.Fatalf("consumed approval was rejected after order %d: %v %v", created.ID, ok, err)
	}
}

func TestExpiredApprovalCannotBeApprovedOrConsumed(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	raw, err := sql.Open("mysql", os.Getenv("STORAGE_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	expire := func(key string) {
		if _, err := raw.Exec("UPDATE paper_order_approval_requests SET expires_at = UTC_TIMESTAMP(6) - INTERVAL 1 SECOND WHERE client_order_id = ?", key); err != nil {
			t.Fatal(err)
		}
	}

	_, key, _ := approvalFixture(t, env, "expired-pending", 12)
	expire(key)
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 13); err != nil || ok {
		t.Fatalf("expired proposal approved: %v %v", ok, err)
	}

	order, key, digest := approvalFixture(t, env, "expired-approved", 12)
	if ok, err := env.db.ApprovePaperOrder(ctx, key, 13); err != nil || !ok {
		t.Fatalf("approve: %v %v", ok, err)
	}
	expire(key)
	if _, _, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest); err == nil {
		t.Fatal("expired approval allowed an order")
	}
	a, _, err := env.db.GetPaperApproval(ctx, key)
	if err != nil || a.Status != storage.PaperApprovalExpired || a.ConsumedOrderID != nil {
		t.Fatalf("expired approval state: %+v %v", a, err)
	}
	_, orders := env.do(t, "GET", "/api/orders", nil)
	if len(listOf(t, orders)) != 0 {
		t.Fatalf("expired approvals must leave funds untouched: %v", orders)
	}
}

// TestPaperOrderStorageOutageLeavesFundsUnchanged simulates the database
// disappearing during submission: the order must fail closed, and a retry
// with the same key after recovery must execute exactly once.
func TestPaperOrderStorageOutageLeavesFundsUnchanged(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	payload := map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 2, "price": 100,
		"client_order_id": fmt.Sprintf("outage-%d", time.Now().UnixNano())}

	if err := env.db.Close(); err != nil {
		t.Fatal(err)
	}
	if code, raw := env.do(t, "POST", "/api/orders", payload); code == http.StatusOK {
		t.Fatalf("order accepted while storage was down: %v", raw)
	}

	recovered, err := storage.Open(os.Getenv("STORAGE_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { recovered.Close() })
	env.engine.db = recovered
	env.db = recovered

	_, orders := env.do(t, "GET", "/api/orders", nil)
	if len(listOf(t, orders)) != 0 {
		t.Fatalf("outage left a partial order behind: %v", orders)
	}
	code, first := env.do(t, "POST", "/api/orders", payload)
	if code != http.StatusOK {
		t.Fatalf("retry after recovery: %d %v", code, first)
	}
	code, second := env.do(t, "POST", "/api/orders", payload)
	if code != http.StatusOK || mapOf(t, second)["replayed"] != true {
		t.Fatalf("second retry must replay, not execute: %d %v", code, second)
	}
	_, orders = env.do(t, "GET", "/api/orders", nil)
	if len(listOf(t, orders)) != 1 {
		t.Fatalf("expected exactly one executed order after outage and retries: %v", orders)
	}
}
