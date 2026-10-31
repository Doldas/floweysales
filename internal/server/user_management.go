package server

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/identity"
)

func sendInvitation(ctx context.Context, cfg identity.SMTPSettings, password, email, token string) error {
	if cfg.Port != 587 {
		return errors.New("SMTP requires port 587 with STARTTLS")
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	ok, _ := client.Extension("STARTTLS")
	if !ok {
		return errors.New("SMTP server does not support STARTTLS")
	}
	if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if cfg.Username != "" {
		if password == "" {
			return errors.New("SMTP password not configured")
		}
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(cfg.Sender); err != nil {
		return err
	}
	if err := client.Rcpt(email); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	link := strings.TrimSuffix(cfg.PublicURL, "/") + "/?invite=" + url.QueryEscape(token)
	boundary := "floweysales-invitation-mixed-2026"
	plain := "You've been invited to FloweySales. Activate your account within 48 hours:\r\n" + link + "\r\n\r\nIf you did not expect this invitation, you can ignore this email.\r\n"
	safeLink := html.EscapeString(link)
	rich := fmt.Sprintf(`<!doctype html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0;padding:32px 12px;background:#0b1020;font-family:Arial,Helvetica,sans-serif;color:#e2e8f0"><table role="presentation" cellpadding="0" cellspacing="0" style="width:100%%;max-width:560px;margin:auto;background:#151d32;border:1px solid #334155;border-radius:16px"><tr><td style="padding:32px"><p style="font-size:19px;font-weight:bold;color:#a5b4fc;margin:0 0 28px">✦ FloweySales</p><h1 style="font-size:27px;line-height:1.25;color:#ffffff;margin:0 0 14px">Your research workspace awaits</h1><p style="color:#cbd5e1;line-height:1.6;font-size:16px">You've been invited to FloweySales. Set your password to activate your private research workspace.</p><p style="margin:30px 0"><a href="%s" style="display:inline-block;padding:14px 24px;background:#6366f1;color:#ffffff;text-decoration:none;font-weight:bold;border-radius:9px">Activate your account</a></p><p style="font-size:13px;color:#a5b4fc">This invitation expires in 48 hours.</p><p style="font-size:13px;color:#94a3b8;line-height:1.6">If the button doesn't work, copy this link:<br><a href="%s" style="color:#67e8f9;overflow-wrap:anywhere">%s</a></p><hr style="border:0;border-top:1px solid #334155;margin:26px 0"><p style="font-size:12px;color:#94a3b8;line-height:1.6">If you weren't expecting this invitation, ignore this message. FloweySales is a research aid, not investment advice.</p></td></tr></table></body></html>`, safeLink, safeLink, safeLink)
	body := "From: " + cfg.Sender + "\r\nTo: " + email + "\r\nSubject: FloweySales account invitation\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + plain + "\r\n--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + rich + "\r\n--" + boundary + "--\r\n"
	_, err = io.WriteString(writer, body)
	if err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
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
