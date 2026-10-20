package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"floweysales/internal/identity"
)

var paperApprovalKey = regexp.MustCompile("^[A-Za-z0-9._:-]{1,128}$")

func paperApprovalDigest(payload map[string]any) (string, string, error) {
	key, ok := payload["client_order_id"].(string)
	if !ok || !paperApprovalKey.MatchString(key) {
		return "", "", fmt.Errorf("client_order_id must be 1-128 safe ASCII characters")
	}
	canonical := make(map[string]any, len(payload))
	for name, value := range payload {
		if name != "client_order_id" { canonical[name] = value }
	}
	raw, err := json.Marshal(canonical)
	if err != nil { return "", "", err }
	return key, fmt.Sprintf("%x", sha256.Sum256(raw)), nil
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
	e.mux.HandleFunc("POST /api/orders/approval-requests", func(w http.ResponseWriter, r *http.Request) {
		requester, ok := authenticate(w,r)
		if !ok { return }
		r.Body = http.MaxBytesReader(w,r.Body,8192)
		defer r.Body.Close()
		var payload map[string]any
		decoder:=json.NewDecoder(r.Body)
		if err:=decoder.Decode(&payload); err!=nil { writeOrderError(w,http.StatusBadRequest,"invalid proposal JSON"); return }
		var tail any
		if decoder.Decode(&tail)!=io.EOF { writeOrderError(w,http.StatusBadRequest,"proposal must contain exactly one JSON object"); return }
		key,digest,err:=paperApprovalDigest(payload)
		if err!=nil { writeOrderError(w,http.StatusBadRequest,err.Error());return }
		if err=e.db.RequestPaperApproval(r.Context(),key,digest,requester);err!=nil {
			writeOrderError(w,http.StatusConflict,"approval request conflict or storage unavailable");return
		}
		writeJSON(w,http.StatusAccepted,map[string]any{"client_order_id":key,"request_hash":digest,"approval_status":"pending"})
	})
	e.mux.HandleFunc("POST /api/orders/approval-requests/{key}/approve",func(w http.ResponseWriter,r *http.Request){
		approver,ok:=authenticate(w,r)
		if !ok {return}
		key:=r.PathValue("key")
		if !paperApprovalKey.MatchString(key) {writeOrderError(w,http.StatusBadRequest,"invalid approval key");return}
		approved,err:=e.db.ApprovePaperOrder(r.Context(),key,approver)
		if err!=nil {writeOrderError(w,http.StatusServiceUnavailable,"approval database unavailable");return}
		if !approved {writeOrderError(w,http.StatusConflict,"request does not exist, is already approved or is consumed");return}
		writeJSON(w,http.StatusOK,map[string]any{"client_order_id":key,"approval_status":"approved","approved_by":approver})
	})
	e.mux.HandleFunc("GET /api/orders/approval-requests/{key}",func(w http.ResponseWriter,r *http.Request){
		_,ok:=authenticate(w,r)
		if !ok {return}
		key:=r.PathValue("key")
		if !paperApprovalKey.MatchString(key) {writeOrderError(w,http.StatusBadRequest,"invalid approval key");return}
		item,found,err:=e.db.GetPaperApproval(r.Context(),key)
		if err!=nil {writeOrderError(w,http.StatusServiceUnavailable,"approval database unavailable");return}
		if !found {writeOrderError(w,http.StatusNotFound,"approval request not found");return}
		writeJSON(w,http.StatusOK,item)
	})
}
