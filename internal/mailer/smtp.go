package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Config is the SMTP account used to send. STARTTLS is always required.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Sender   string // bare address used as From and envelope sender
}

func validAddress(a string) (string, error) {
	if strings.ContainsAny(a, "\r\n") {
		return "", errors.New("invalid email address")
	}
	p, err := mail.ParseAddress(a)
	if err != nil {
		return "", errors.New("invalid email address")
	}
	return p.Address, nil
}

// Build renders the message and assembles a multipart/alternative RFC 5322
// message with a text/plain and a text/html part.
func Build(sender, recipient string, msg Message) ([]byte, error) {
	from, err := validAddress(sender)
	if err != nil {
		return nil, err
	}
	to, err := validAddress(recipient)
	if err != nil {
		return nil, err
	}
	r, err := Render(msg)
	if err != nil {
		return nil, err
	}
	sentAt := msg.SentAt
	if sentAt.IsZero() {
		sentAt = time.Now()
	}
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(raw[:])
	boundary := "fs-" + id[:24]
	domain := "floweysales.local"
	if i := strings.LastIndexByte(from, '@'); i >= 0 && i+1 < len(from) {
		domain = from[i+1:]
	}
	var b bytes.Buffer
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", r.Subject) + "\r\n")
	b.WriteString("Date: " + sentAt.UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + id[24:] + "." + id[:12] + "@" + domain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	for _, part := range []struct{ typ, body string }{{"text/plain", r.Text}, {"text/html", r.HTML}} {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + part.typ + "; charset=UTF-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		w := quotedprintable.NewWriter(&b)
		if _, err := w.Write([]byte(strings.ReplaceAll(part.body, "\r\n", "\n"))); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
}

// Send renders and delivers one message. It is the only SMTP entry point of
// the application. Errors never contain message content.
func Send(ctx context.Context, cfg Config, recipient string, msg Message) error {
	sender, err := validAddress(cfg.Sender)
	if err != nil {
		return err
	}
	to, err := validAddress(recipient)
	if err != nil {
		return err
	}
	data, err := Build(sender, to, msg)
	if err != nil {
		return err
	}
	if cfg.Host == "" || cfg.Port <= 0 {
		return errors.New("SMTP host and port are required")
	}
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("smtp dial failed: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("SMTP server does not support STARTTLS")
	}
	if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("STARTTLS failed: %w", err)
	}
	if cfg.Username != "" {
		if cfg.Password == "" {
			return errors.New("SMTP password not configured")
		}
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}
	}
	if err := client.Mail(sender); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}
