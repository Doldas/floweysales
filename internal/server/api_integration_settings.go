package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"floweysales/internal/sec"
	"floweysales/internal/storage"
)

const apiSecretEnv = "FLOWEYSALES_SECRETS_KEY"

// Secret encryption is required for persistent credentials. The env value is
// base64-encoded 32 random bytes. It is never accepted through the web UI.
func apiSecretCipher() (cipher.AEAD, error) {
	raw := strings.TrimSpace(os.Getenv(apiSecretEnv))
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, errors.New("server secret encryption key is not configured")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func sealAPISecret(kind, secret string) ([]byte, error) {
	aead, err := apiSecretCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, []byte(secret), []byte(kind))...), nil
}
func openAPISecret(kind string, data []byte) (string, error) {
	aead, err := apiSecretCipher()
	if err != nil {
		return "", err
	}
	if len(data) < aead.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	plaintext, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(kind))
	if err != nil {
		return "", errors.New("cannot decrypt saved integration secret")
	}
	return string(plaintext), nil
}

// RestoreAPISecrets is called during normal AI config restoration at startup.
func (e *Engine) RestoreAPISecrets(ctx context.Context) error {
	for _, kind := range []string{"ai", "web_search", "sec"} {
		entry, err := e.db.GetAPIIntegrationSecret(ctx, kind)
		if storage.IsMissingAPIIntegration(err) {
			continue
		}
		if err != nil {
			return err
		}
		secret, err := openAPISecret(kind, entry.Ciphertext)
		if err != nil {
			return fmt.Errorf("%s credential restoration: %w", kind, err)
		}
		switch kind {
		case "ai":
			base, _, model := e.aiDefaults()
			e.ConfigureAI(base, secret, model)
		case "sec":
			e.setSECContact(secret)
		default:
			e.integrationMu.Lock()
			e.webSearchProvider = entry.Provider
			e.webSearchAPIKey = registeredSecret(secret)
			e.integrationMu.Unlock()
		}
	}
	return nil
}

// setSECContact makes the SEC client identify itself with the saved contact;
// an empty contact goes back to SEC_USER_AGENT from the environment.
func (e *Engine) setSECContact(userAgent string) {
	e.integrationMu.Lock()
	e.secContact = userAgent
	e.integrationMu.Unlock()
	if userAgent == "" {
		userAgent = os.Getenv("SEC_USER_AGENT")
	}
	sec.Production().SetUserAgent(userAgent)
}
func (e *Engine) registerIntegrationSettingsRoutes() {
	e.mux.HandleFunc("GET /api/admin/integrations", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		_, err := apiSecretCipher()
		_, aiKey, _ := e.aiDefaults()
		e.integrationMu.Lock()
		provider := e.webSearchProvider
		webHasKey := e.webSearchAPIKey != ""
		secSaved := e.secContact
		e.integrationMu.Unlock()
		secSource := "none"
		switch ua := sec.Production().UserAgentValue(); {
		case secSaved != "":
			secSource = "saved"
		case strings.Contains(ua, "@"):
			secSource = "environment"
		}
		writeJSON(w, 200, map[string]any{
			"encryption_ready": err == nil,
			"ai":               map[string]any{"has_key": aiKey != ""},
			"web_search":       map[string]any{"provider": provider, "has_key": webHasKey},
			"sec":              map[string]any{"configured": secSource != "none", "source": secSource, "contact": secSaved},
		})
	})
	e.mux.HandleFunc("PUT /api/admin/integrations/{kind}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		kind := r.PathValue("kind")
		if kind != "ai" && kind != "web_search" && kind != "sec" {
			writeJSON(w, 404, map[string]string{"error": "Unknown integration"})
			return
		}
		if _, err := apiSecretCipher(); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Server secret encryption key is unavailable; check server startup configuration"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		defer r.Body.Close()
		var body struct {
			APIKey   string `json:"api_key"`
			Provider string `json:"provider"`
			Contact  string `json:"contact"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid integration settings"})
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "Unexpected request body content"})
			return
		}
		if kind == "sec" {
			userAgent, err := sec.ContactUserAgent(body.Contact)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			encrypted, err := sealAPISecret(kind, userAgent)
			if err != nil {
				writeJSON(w, 503, map[string]string{"error": "Credential encryption unavailable"})
				return
			}
			if err := e.db.SaveAPIIntegrationSecret(r.Context(), kind, "sec_edgar", encrypted); err != nil {
				writeJSON(w, 503, map[string]string{"error": "Unable to save the SEC contact"})
				return
			}
			e.setSECContact(userAgent)
			writeJSON(w, 200, map[string]any{"saved": true, "contact": userAgent})
			return
		}
		body.APIKey = strings.TrimSpace(body.APIKey)
		if body.APIKey == "" || len(body.APIKey) > 4096 || strings.ContainsAny(body.APIKey, "\r\n") {
			writeJSON(w, 400, map[string]string{"error": "Invalid API key"})
			return
		}
		if kind == "ai" {
			body.Provider = "openai_compatible"
		}
		if kind == "web_search" && body.Provider != "brave" && body.Provider != "tavily" {
			writeJSON(w, 400, map[string]string{"error": "Web search provider must be brave or tavily"})
			return
		}
		encrypted, err := sealAPISecret(kind, body.APIKey)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Credential encryption unavailable"})
			return
		}
		if err := e.db.SaveAPIIntegrationSecret(r.Context(), kind, body.Provider, encrypted); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Unable to save encrypted credentials"})
			return
		}
		if kind == "ai" {
			base, _, model := e.aiDefaults()
			e.ConfigureAI(base, body.APIKey, model)
		} else {
			e.integrationMu.Lock()
			e.webSearchProvider = body.Provider
			e.webSearchAPIKey = registeredSecret(body.APIKey)
			e.integrationMu.Unlock()
		}
		writeJSON(w, 200, map[string]any{"saved": true, "has_key": true, "provider": body.Provider})
	})
	e.mux.HandleFunc("DELETE /api/admin/integrations/{kind}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		kind := r.PathValue("kind")
		if kind != "ai" && kind != "web_search" && kind != "sec" {
			writeJSON(w, 404, map[string]string{"error": "Unknown integration"})
			return
		}
		if err := e.db.DeleteAPIIntegrationSecret(r.Context(), kind); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Unable to remove credential"})
			return
		}
		switch kind {
		case "ai":
			base, _, model := e.aiDefaults()
			e.ConfigureAI(base, "", model)
		case "sec":
			e.setSECContact("")
		default:
			e.integrationMu.Lock()
			e.webSearchProvider = ""
			e.webSearchAPIKey = ""
			e.integrationMu.Unlock()
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
}
