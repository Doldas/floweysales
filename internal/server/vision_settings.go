package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"floweysales/internal/redact"
	"floweysales/internal/storage"
	"floweysales/internal/vision"
)

// visionKind is the name under which the vision model setting is stored in the
// encrypted integration table. The whole setting, endpoint and key included,
// is one sealed value, so the key never sits next to readable configuration.
const visionKind = "vision"

// visionState is the vision model setting held in memory, plus the telemetry
// of paid vision calls. Telemetry never contains document content.
type visionState struct {
	mu    sync.RWMutex
	cfg   vision.Config
	cache *vision.Cache
	stats visionStats
}

type visionStats struct {
	Calls, Failures, CacheHits, Images, Tokens int
	TokensUnknownCalls                         int
	LastErrorCode                              string
	LastErrorAt                                string
}

func (s *visionState) config() vision.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *visionState) set(c vision.Config) {
	s.mu.Lock()
	changed := s.cfg.Fingerprint() != c.Fingerprint() || s.cfg.APIKey != c.APIKey
	s.cfg = c
	if changed && s.cache != nil {
		s.cache.Purge() // results of another model or endpoint are not reused
	}
	s.mu.Unlock()
	redact.RegisterSecret(c.APIKey)
}

func (s *visionState) resultCache() *vision.Cache {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = vision.NewCache(256, 12*time.Hour)
	}
	return s.cache
}

func (s *visionState) record(f func(*visionStats)) {
	s.mu.Lock()
	f(&s.stats)
	s.mu.Unlock()
}

func (s *visionState) snapshot() visionStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// visionConfig returns the current setting (the key included): server-side use only.
func (e *Engine) visionConfig() vision.Config { return e.vision.config() }

// restoreVision loads the sealed setting at startup.
func (e *Engine) restoreVision(ctx context.Context) error {
	entry, err := e.db.GetAPIIntegrationSecret(ctx, visionKind)
	if storage.IsMissingAPIIntegration(err) {
		return nil
	}
	if err != nil {
		return err
	}
	plain, err := openAPISecret(visionKind, entry.Ciphertext)
	if err != nil {
		return err
	}
	var cfg vision.Config
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return err
	}
	if cfg, err = cfg.Validate(); err != nil {
		return err
	}
	e.vision.set(cfg)
	return nil
}

func (e *Engine) saveVision(ctx context.Context, cfg vision.Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	sealed, err := sealAPISecret(visionKind, string(raw))
	if err != nil {
		return err
	}
	if err := e.db.SaveAPIIntegrationSecret(ctx, visionKind, "openai_compatible", sealed); err != nil {
		return err
	}
	e.vision.set(cfg)
	return nil
}

// visionView is what an administrator sees: the setting without the key.
func (e *Engine) visionView() map[string]any {
	cfg := e.vision.config()
	st := e.vision.snapshot()
	_, encErr := apiSecretCipher()
	return map[string]any{
		"encryption_ready": encErr == nil,
		"status":           cfg.Status(),
		"enabled":          cfg.Enabled,
		"base_url":         cfg.BaseURL,
		"model":            cfg.Model,
		"fallback_model":   cfg.FallbackModel,
		"supports_images":  cfg.SupportsImages,
		"has_key":          cfg.APIKey != "",
		"verified":         cfg.Verified(),
		"verified_at":      cfg.VerifiedAt,
		"limits":           cfg.Limits,
		"defaults":         vision.DefaultLimits(),
		"telemetry": map[string]any{
			"calls": st.Calls, "failures": st.Failures, "cache_hits": st.CacheHits,
			"images": st.Images, "tokens": st.Tokens, "calls_without_usage": st.TokensUnknownCalls,
			"last_error_code": st.LastErrorCode, "last_error_at": st.LastErrorAt,
		},
	}
}

func (e *Engine) registerVisionRoutes() {
	e.mux.HandleFunc("GET /api/admin/vision", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, e.visionView())
	})
	// Any signed-in user may learn whether scanned documents can be read, and
	// nothing else: no endpoint, model or key.
	e.mux.HandleFunc("GET /api/vision/status", func(w http.ResponseWriter, r *http.Request) {
		if e.authService != nil {
			if _, ok := e.currentUser(r); !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]bool{"available": e.vision.config().Usable()})
	})
	e.mux.HandleFunc("PUT /api/admin/vision", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		if _, err := apiSecretCipher(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Server secret encryption key is unavailable; check server startup configuration"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		defer r.Body.Close()
		var body struct {
			Enabled        bool          `json:"enabled"`
			BaseURL        string        `json:"base_url"`
			Model          string        `json:"model"`
			FallbackModel  string        `json:"fallback_model"`
			SupportsImages bool          `json:"supports_images"`
			APIKey         string        `json:"api_key"`
			ClearKey       bool          `json:"clear_key"`
			Limits         vision.Limits `json:"limits"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid vision settings"})
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unexpected request body content"})
			return
		}
		old := e.vision.config()
		next := vision.Config{
			Enabled: body.Enabled, BaseURL: body.BaseURL, Model: body.Model, FallbackModel: body.FallbackModel,
			SupportsImages: body.SupportsImages, APIKey: body.APIKey, Limits: body.Limits,
			VerifiedFingerprint: old.VerifiedFingerprint, VerifiedAt: old.VerifiedAt,
		}
		next, err := next.Validate()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		switch {
		case body.ClearKey:
			next.APIKey = ""
		case next.APIKey == "" && next.BaseURL == old.BaseURL:
			next.APIKey = old.APIKey
		}
		// A key saved for one endpoint is never sent to another: pointing the
		// setting elsewhere requires entering the key again.
		if err := e.saveVision(r.Context(), next); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Unable to save the vision model settings"})
			return
		}
		writeJSON(w, http.StatusOK, e.visionView())
	})
	e.mux.HandleFunc("DELETE /api/admin/vision", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		if err := e.db.DeleteAPIIntegrationSecret(r.Context(), visionKind); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Unable to remove the vision model settings"})
			return
		}
		e.vision.set(vision.Config{})
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})
	e.mux.HandleFunc("POST /api/admin/vision/test", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		cfg := e.vision.config()
		if cfg.BaseURL == "" || cfg.Model == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Save an endpoint and a model first"})
			return
		}
		if !cfg.SupportsImages {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Confirm that the model accepts image input before testing it"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		res := vision.NewClient(cfg).Probe(ctx)
		if res.OK {
			cfg.VerifiedFingerprint, cfg.VerifiedAt = cfg.Fingerprint(), time.Now().UTC().Format(time.RFC3339)
		} else {
			cfg.VerifiedFingerprint, cfg.VerifiedAt = "", ""
		}
		if err := e.saveVision(r.Context(), cfg); err != nil {
			log.Printf("[Vision] test result not saved: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": res, "settings": e.visionView()})
	})
}
