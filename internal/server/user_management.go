package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"floweysales/internal/identity"
	"floweysales/internal/mailer"
)

func sendInvitation(ctx context.Context, cfg identity.SMTPSettings, password, email, token string) error {
	if cfg.Port != 587 {
		return errors.New("SMTP requires port 587 with STARTTLS")
	}
	link := strings.TrimSuffix(cfg.PublicURL, "/") + "/?invite=" + url.QueryEscape(token)
	return mailer.Send(ctx, smtpConfig(cfg, password), email, mailer.Message{
		Kind:        mailer.KindTransactional,
		Subject:     "FloweySales account invitation",
		Title:       "Your research workspace awaits",
		Preheader:   "You've been invited to FloweySales.",
		ContentType: mailer.Text,
		Content:     "You've been invited to FloweySales. Set your password to activate your private research workspace.",
		Action:      &mailer.Action{Label: "Activate your account", URL: link},
		ActionNote:  "This invitation expires in 48 hours.",
	})
}

// smtpConfig adapts the stored mail settings and the opened password.
func smtpConfig(cfg identity.SMTPSettings, password string) mailer.Config {
	return mailer.Config{Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: password, Sender: cfg.Sender}
}

func validSMTP(cfg identity.SMTPSettings) bool {
	if cfg.Port != 587 || len(cfg.Host) < 3 || len(cfg.Host) > 253 || strings.ContainsAny(cfg.Host, " /\\@:\r\n") {
		return false
	}
	address, err := mail.ParseAddress(cfg.Sender)
	if err != nil || address.Address != cfg.Sender || strings.ContainsAny(cfg.Sender, "\r\n") {
		return false
	}
	if len(cfg.Username) > 254 || strings.ContainsAny(cfg.Username, "\r\n") {
		return false
	}
	u, err := url.Parse(cfg.PublicURL)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(cfg.PublicURL) <= 512
}

func (e *Engine) registerUserManagementRoutes() {
	e.mux.HandleFunc("GET /api/admin/roles", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		roles, err := e.authService.ListRoles(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Cannot load roles"})
			return
		}
		writeJSON(w, 200, map[string]any{"roles": roles, "permissions": []string{"research.read", "ai.chat", "models.use", "flows.read", "flows.write", "flows.run"}})
	})
	e.mux.HandleFunc("POST /api/admin/roles", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		defer r.Body.Close()
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid role"})
			return
		}
		if err := e.authService.AddRole(r.Context(), req.Name); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Role must be unique, uppercase letters/underscores, length 2–40"})
			return
		}
		writeJSON(w, 201, map[string]bool{"created": true})
	})
	e.mux.HandleFunc("PUT /api/admin/roles/{name}/permissions/{permission}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		defer r.Body.Close()
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid grant"})
			return
		}
		if err := e.authService.SetRolePermission(r.Context(), r.PathValue("name"), r.PathValue("permission"), req.Enabled); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid role or permission"})
			return
		}
		writeJSON(w, 200, map[string]bool{"updated": true})
	})
	e.mux.HandleFunc("GET /api/admin/users/list", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		users, err := e.authService.ListUsers(r.Context())
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Unable to list users"})
			return
		}
		writeJSON(w, 200, map[string]any{"users": users})
	})
	e.mux.HandleFunc("POST /api/admin/users/invite", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		defer r.Body.Close()
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid email"})
			return
		}
		cfg, err := e.authService.GetSMTPSettings(r.Context())
		if err != nil || !validSMTP(cfg) {
			writeJSON(w, 503, map[string]string{"error": "Configure SMTP mail delivery in Settings before inviting users"})
			return
		}
		secret, err := e.db.GetAPIIntegrationSecret(r.Context(), "smtp")
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Configure an encrypted SMTP password"})
			return
		}
		password, err := openAPISecret("smtp", secret.Ciphertext)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "SMTP credential unavailable"})
			return
		}
		email, err := identity.NormalizeEmail(req.Email)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid email"})
			return
		}
		token, err := e.authService.InviteUser(r.Context(), email)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "Could not invite account; address may already exist"})
			return
		}
		if err := sendInvitation(r.Context(), cfg, password, email, token); err != nil {
			_ = e.authService.RevokeInvitation(r.Context(), token)
			writeJSON(w, 502, map[string]string{"error": "SMTP delivery failed; verify SMTP configuration"})
			return
		}
		writeJSON(w, 201, map[string]bool{"invited": true})
	})
	e.mux.HandleFunc("DELETE /api/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Invalid user ID"})
			return
		}
		actor, ok := e.currentUser(r)
		if !ok || actor.ID == id {
			writeJSON(w, 409, map[string]string{"error": "Cannot delete your own account"})
			return
		}
		if err := e.authService.DeleteUser(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeJSON(w, 404, map[string]string{"error": "User not found"})
				return
			}
			writeJSON(w, 409, map[string]string{"error": "Unable to delete user. Accounts with protected dependent records must be handled before deletion."})
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
	e.mux.HandleFunc("PATCH /api/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Invalid user ID"})
			return
		}
		actor, _ := e.currentUser(r)
		if actor.ID == id {
			writeJSON(w, 409, map[string]string{"error": "Cannot edit own role or status"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		defer r.Body.Close()
		var data struct {
			Role   *string `json:"role"`
			Active *bool   `json:"active"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&data); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid account change"})
			return
		}
		if data.Role == nil && data.Active == nil {
			writeJSON(w, 400, map[string]string{"error": "No changes supplied"})
			return
		}
		if data.Role != nil {
			if err := e.authService.UpdateUserRole(r.Context(), id, *data.Role); err != nil {
				writeJSON(w, 400, map[string]string{"error": "Invalid role"})
				return
			}
		}
		if data.Active != nil {
			if err := e.authService.SetUserActive(r.Context(), id, *data.Active); err != nil {
				writeJSON(w, 400, map[string]string{"error": "Invalid account status"})
				return
			}
		}
		writeJSON(w, 200, map[string]bool{"updated": true})
	})
	e.mux.HandleFunc("GET /api/admin/settings/smtp", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		cfg, err := e.authService.GetSMTPSettings(r.Context())
		if errors.Is(err, sql.ErrNoRows) {
			cfg = identity.SMTPSettings{Port: 587}
		} else if err != nil {
			writeJSON(w, 503, map[string]string{"error": "SMTP configuration unavailable"})
			return
		}
		_, err = e.db.GetAPIIntegrationSecret(r.Context(), "smtp")
		writeJSON(w, 200, map[string]any{"configuration": cfg, "has_password": err == nil})
	})
	e.mux.HandleFunc("PUT /api/admin/settings/smtp", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()
		var cfg identity.SMTPSettings
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil || !validSMTP(cfg) {
			writeJSON(w, 400, map[string]string{"error": "Valid SMTP host, port 587, sender, username and public HTTPS URL are required"})
			return
		}
		if err := e.authService.SaveSMTPSettings(r.Context(), cfg); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not save SMTP settings"})
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
	e.mux.HandleFunc("PUT /api/admin/settings/smtp/password", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		defer r.Body.Close()
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Password) < 1 || len(req.Password) > 4096 {
			writeJSON(w, 400, map[string]string{"error": "Invalid SMTP password"})
			return
		}
		ciphertext, err := sealAPISecret("smtp", req.Password)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Server encryption key is not configured"})
			return
		}
		if err := e.db.SaveAPIIntegrationSecret(r.Context(), "smtp", "smtp", ciphertext); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Could not save SMTP credentials"})
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	})
}
