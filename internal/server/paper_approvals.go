package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"floweysales/internal/identity"
	"floweysales/internal/storage"
)

var (
	paperApprovalKey    = regexp.MustCompile("^[A-Za-z0-9._:-]{1,128}$")
	paperDecisionSource = regexp.MustCompile("^[A-Za-z0-9._:/-]{1,128}$")
)

const (
	maxApprovalRationale = 2000
	maxRejectionReason   = 500
)

// paperReviewFields describe the decision behind a proposal. They are
// recorded for audit but are not order parameters, so they never take part
// in the request hash.
var paperReviewFields = map[string]bool{"client_order_id": true, "rationale": true, "decision_source": true}

// paperRequestDigest hashes the order parameters of a request.
func paperRequestDigest(payload map[string]any) (string, error) {
	canonical := make(map[string]any, len(payload))
	for name, value := range payload {
		if !paperReviewFields[name] {
			canonical[name] = value
		}
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func paperApprovalDigest(payload map[string]any) (string, string, error) {
	key, ok := payload["client_order_id"].(string)
	if !ok || !paperApprovalKey.MatchString(key) {
		return "", "", fmt.Errorf("client_order_id must be 1-128 safe ASCII characters")
	}
	digest, err := paperRequestDigest(payload)
	if err != nil {
		return "", "", err
	}
	return key, digest, nil
}

// paperDecisionContext validates the human-readable rationale and the origin
// of the recommendation that a proposal must carry.
func paperDecisionContext(payload map[string]any) (rationale, source string, err error) {
	rationale, _ = payload["rationale"].(string)
	rationale = strings.TrimSpace(rationale)
	if rationale == "" {
		return "", "", fmt.Errorf("rationale is required to explain the decision behind the proposal")
	}
	if utf8.RuneCountInString(rationale) > maxApprovalRationale || !utf8.ValidString(rationale) {
		return "", "", fmt.Errorf("rationale must be valid text of at most %d characters", maxApprovalRationale)
	}
	source = "manual"
	if raw, present := payload["decision_source"]; present {
		s, ok := raw.(string)
		if !ok || !paperDecisionSource.MatchString(s) {
			return "", "", fmt.Errorf("decision_source must be 1-128 safe ASCII characters, e.g. manual or flow-run:42")
		}
		source = s
	}
	return rationale, source, nil
}

// paperApprovalAudit is the decision → approval → order chain for one key.
type paperApprovalAudit struct {
	storage.PaperApproval
	Order       *storage.Order            `json:"order,omitempty"`
	OrderEvents []storage.PaperOrderEvent `json:"order_events,omitempty"`
}

func (e *Engine) registerPaperApprovalRoutes() {
	// Proposals and approvals require live SYS_ADMIN sessions even if the
	// application's optional auth mode was not configured.
	authenticate := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		if e.authService == nil {
			writeOrderError(w, http.StatusServiceUnavailable, "enable authentication before paper approval")
			return 0, false
		}
		user, ok := e.currentUser(r)
		if !ok {
			writeOrderError(w, http.StatusUnauthorized, "authentication required")
			return 0, false
		}
		if user.Role != identity.RoleSysAdmin {
			writeOrderError(w, http.StatusForbidden, "SYS_ADMIN approval required")
			return 0, false
		}
		return user.ID, true
	}
	pathKey := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		key := r.PathValue("key")
		if !paperApprovalKey.MatchString(key) {
			writeOrderError(w, http.StatusBadRequest, "invalid approval key")
			return "", false
		}
		return key, true
	}
	e.mux.HandleFunc("POST /api/paper-approvals", func(w http.ResponseWriter, r *http.Request) {
		requester, ok := authenticate(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		defer r.Body.Close()
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&payload); err != nil {
			writeOrderError(w, http.StatusBadRequest, "invalid proposal JSON")
			return
		}
		var tail any
		if decoder.Decode(&tail) != io.EOF {
			writeOrderError(w, http.StatusBadRequest, "proposal must contain exactly one JSON object")
			return
		}
		key, digest, err := paperApprovalDigest(payload)
		if err != nil {
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}
		rationale, source, err := paperDecisionContext(payload)
		if err != nil {
			writeOrderError(w, http.StatusBadRequest, err.Error())
			return
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			writeOrderError(w, http.StatusBadRequest, "invalid approval payload")
			return
		}
		err = e.db.RequestPaperApproval(r.Context(), storage.PaperApprovalProposal{
			Key: key, Digest: digest, Payload: string(payloadBytes), Rationale: rationale,
			DecisionSource: source, RequestedBy: requester, TTLSeconds: e.tradingConfig().ApprovalTTLSeconds(),
		})
		if err != nil {
			writeOrderError(w, http.StatusConflict, "approval request conflict or storage unavailable")
			return
		}
		// A repeated proposal is idempotent; report the stored state, which
		// may already be approved, rejected or expired.
		item, found, err := e.db.GetPaperApproval(r.Context(), key)
		if err != nil || !found {
			writeOrderError(w, http.StatusServiceUnavailable, "approval database unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"client_order_id": key, "request_hash": digest,
			"approval_status": item.Status, "expires_at": item.ExpiresAt})
	})
	e.mux.HandleFunc("POST /api/paper-approvals/{key}/approve", func(w http.ResponseWriter, r *http.Request) {
		approver, ok := authenticate(w, r)
		if !ok {
			return
		}
		key, ok := pathKey(w, r)
		if !ok {
			return
		}
		approved, err := e.db.ApprovePaperOrder(r.Context(), key, approver)
		if err != nil {
			writeOrderError(w, http.StatusServiceUnavailable, "approval database unavailable")
			return
		}
		if !approved {
			// Explain the refusal; the conditional update already decided it.
			item, found, err := e.db.GetPaperApproval(r.Context(), key)
			switch {
			case err != nil:
				writeOrderError(w, http.StatusServiceUnavailable, "approval database unavailable")
			case !found:
				writeOrderError(w, http.StatusNotFound, "approval request not found")
			case item.Status == storage.PaperApprovalPending && item.RequestedBy == approver:
				writeOrderError(w, http.StatusForbidden, "a different SYS_ADMIN than the requester must approve")
			default:
				writeOrderError(w, http.StatusConflict, "request is "+item.Status+" and cannot be approved")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"client_order_id": key, "approval_status": storage.PaperApprovalApproved, "approved_by": approver})
	})
	e.mux.HandleFunc("POST /api/paper-approvals/{key}/reject", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authenticate(w, r)
		if !ok {
			return
		}
		key, ok := pathKey(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()
		var body struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeOrderError(w, http.StatusBadRequest, "invalid rejection JSON")
			return
		}
		reason := strings.TrimSpace(body.Reason)
		if reason == "" || utf8.RuneCountInString(reason) > maxRejectionReason {
			writeOrderError(w, http.StatusBadRequest, fmt.Sprintf("reason is required (at most %d characters)", maxRejectionReason))
			return
		}
		rejected, err := e.db.RejectPaperOrder(r.Context(), key, actor, reason)
		if err != nil {
			writeOrderError(w, http.StatusServiceUnavailable, "approval database unavailable")
			return
		}
		if !rejected {
			writeOrderError(w, http.StatusConflict, "request does not exist, is already rejected or is consumed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"client_order_id": key, "approval_status": storage.PaperApprovalRejected, "rejected_by": actor})
	})
	e.mux.HandleFunc("GET /api/paper-approvals", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authenticate(w, r); !ok {
			return
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = storage.PaperApprovalPending
		}
		if !storage.ValidPaperApprovalFilter(status) {
			writeOrderError(w, http.StatusBadRequest, "status must be pending, approved, rejected, consumed, expired or all")
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				writeOrderError(w, http.StatusBadRequest, "limit must be between 1 and 100")
				return
			}
			limit = n
		}
		items, err := e.db.ListPaperApprovals(r.Context(), status, limit)
		if err != nil {
			writeOrderError(w, http.StatusServiceUnavailable, "approval database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": status, "approvals": items})
	})
	e.mux.HandleFunc("GET /api/paper-approvals/{key}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authenticate(w, r); !ok {
			return
		}
		key, ok := pathKey(w, r)
		if !ok {
			return
		}
		item, found, err := e.db.GetPaperApproval(r.Context(), key)
		if err != nil {
			writeOrderError(w, http.StatusServiceUnavailable, "approval database unavailable")
			return
		}
		if !found {
			writeOrderError(w, http.StatusNotFound, "approval request not found")
			return
		}
		audit := paperApprovalAudit{PaperApproval: item}
		if item.ConsumedOrderID != nil {
			id := int(*item.ConsumedOrderID)
			order, found := e.db.GetOrder(id)
			if found {
				audit.Order = &order
			}
			events, _, err := e.db.GetPaperOrderEvents(r.Context(), id)
			if err != nil {
				writeOrderError(w, http.StatusServiceUnavailable, "order audit storage unavailable")
				return
			}
			audit.OrderEvents = events
		}
		writeJSON(w, http.StatusOK, audit)
	})
}
