package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
)

// Evidence settings: the mode and limits of the citation check (see
// docs/agent-evidence.md). Each user saves their own default for chat and team
// runs; a request, a team run or a flow agent node can name its own. An
// administrator has no say in them.

// evidenceDefaultStore keeps each user's saved default.
type evidenceDefaultStore interface {
	LoadEvidenceDefault(ctx context.Context, userID int64) (string, error)
	SaveEvidenceDefault(ctx context.Context, userID int64, policy string) error
	DeleteEvidenceDefault(ctx context.Context, userID int64) error
}

func (e *Engine) evidenceStore() evidenceDefaultStore {
	if e.evidenceDefaults != nil {
		return e.evidenceDefaults
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

// savedEvidencePolicy is the user's saved default, or the zero policy when
// there is none or it can no longer be read.
func (e *Engine) savedEvidencePolicy(ctx context.Context, userID int64) ai.EvidencePolicy {
	store := e.evidenceStore()
	if store == nil {
		return ai.EvidencePolicy{}
	}
	raw, err := store.LoadEvidenceDefault(ctx, userID)
	if err != nil || raw == "" {
		return ai.EvidencePolicy{}
	}
	var p ai.EvidencePolicy
	if json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
		return ai.EvidencePolicy{}
	}
	return p
}

// requestEvidencePolicy works out the policy of a chat or team run. A full
// policy in the request wins; a bare evidence_mode means that mode with no
// limits; a request that names neither gets the caller's saved default. The
// second result is the problem to report with a 400, or "".
func (e *Engine) requestEvidencePolicy(ctx context.Context, userID int64, mode string, full *ai.EvidencePolicy) (ai.EvidencePolicy, string) {
	var p ai.EvidencePolicy
	switch {
	case full != nil:
		p = *full
		if p.Mode == "" {
			p.Mode = mode
		} else if mode != "" && mode != p.Mode {
			return p, "evidence_mode and evidence.mode disagree"
		}
	case mode != "":
		p = ai.EvidencePolicy{Mode: mode}
	default:
		p = e.savedEvidencePolicy(ctx, userID)
	}
	if err := p.Validate(); err != nil {
		return p, err.Error()
	}
	return p, ""
}

// evidenceUserID is the caller's account; with authentication off the single
// user is 0, as for the other per-user settings.
func (e *Engine) evidenceUserID(r *http.Request) int64 {
	if e.authService == nil {
		return 0
	}
	if u, ok := e.currentUser(r); ok {
		return u.ID
	}
	return 0
}

// registerEvidenceRoutes serves the caller's saved evidence default.
func (e *Engine) registerEvidenceRoutes() {
	respond := func(w http.ResponseWriter, p *ai.EvidencePolicy) {
		writeJSON(w, http.StatusOK, map[string]any{
			"policy":       p,
			"source_types": []string{sources.ClassPrimaryFiling, sources.ClassWebPage},
			"max_age_days": ai.MaxEvidenceAgeDays,
		})
	}
	e.mux.HandleFunc("GET /api/ai/evidence-default", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		store := e.evidenceStore()
		if store == nil {
			respond(w, nil)
			return
		}
		raw, err := store.LoadEvidenceDefault(r.Context(), u.ID)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Evidence settings unavailable"})
			return
		}
		var p ai.EvidencePolicy
		if raw == "" || json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
			respond(w, nil)
			return
		}
		respond(w, &p)
	})
	e.mux.HandleFunc("PUT /api/ai/evidence-default", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		var p ai.EvidencePolicy
		if !decodeStrategyBody(w, r, &p) {
			return
		}
		if err := p.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		store := e.evidenceStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Evidence settings storage unavailable"})
			return
		}
		raw, _ := json.Marshal(p)
		if err := store.SaveEvidenceDefault(r.Context(), u.ID, string(raw)); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot save the evidence settings"})
			return
		}
		respond(w, &p)
	})
	e.mux.HandleFunc("DELETE /api/ai/evidence-default", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if store := e.evidenceStore(); store != nil {
			if err := store.DeleteEvidenceDefault(r.Context(), u.ID); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot reset the evidence settings"})
				return
			}
		}
		respond(w, nil)
	})
}

// CheckLink tells whether a cited address still resolves, for the reachability
// check of the evidence modes. It uses the same address checks as read_source
// and the flow HTTP node. An address those checks refuse (local, private,
// credentials) is not checked rather than reported dead, since the question
// whether it resolves was never asked of the network.
func (s engineSource) CheckLink(ctx context.Context, raw string) error {
	return checkPublicLink(ctx, raw)
}

// linkStatusError is nil when the server answered in a way that means the
// page exists. 401, 403 and 429 mean it answered but will not talk to an
// automated client, which is not a dead link.
func linkStatusError(code int) error {
	switch {
	case code >= 200 && code < 300, code == http.StatusUnauthorized, code == http.StatusForbidden, code == http.StatusTooManyRequests:
		return nil
	}
	return fmt.Errorf("HTTP status %d", code)
}

func checkPublicLink(ctx context.Context, raw string) error {
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	current := raw
	for hop := 0; hop <= readPageMaxHops; hop++ {
		u, err := flowPublicURL(current)
		if err != nil {
			return nil // not an address this server may open; not checked
		}
		client, closeClient, err := flowPinnedClient(ctx, u)
		if err != nil {
			if strings.Contains(err.Error(), "forbidden network") {
				return nil
			}
			return err
		}
		code, location, err := probeLink(ctx, client, u.String())
		closeClient()
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		if code >= 300 && code < 400 {
			next, perr := url.Parse(location)
			if location == "" || perr != nil {
				return errors.New("redirected without a usable address")
			}
			current = u.ResolveReference(next).String()
			continue
		}
		return linkStatusError(code)
	}
	return errors.New("redirected too many times")
}

// probeLink asks with HEAD and, for a server that does not support it, with
// GET; the body is never read.
func probeLink(ctx context.Context, client *http.Client, address string) (int, string, error) {
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequestWithContext(ctx, method, address, nil)
		if err != nil {
			return 0, "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		resp.Body.Close()
		if method == http.MethodHead && (resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented) {
			continue
		}
		return resp.StatusCode, resp.Header.Get("Location"), nil
	}
	return 0, "", errors.New("no answer")
}
