package notify

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// EmailNotifier sends alert emails asynchronously via SMTP with STARTTLS.
type EmailNotifier struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	AppPassword  string
	TargetEmail  string
}

// NewEmailNotifier creates a notifier. The host/port default to Gmail's SMTP
// relay, with simple heuristics for other common providers.
func NewEmailNotifier(username, password, receiver string) *EmailNotifier {
	host, port := "smtp.gmail.com", 587
	if strings.Contains(username, "@") {
		switch {
		case strings.HasSuffix(username, "@outlook.com"), strings.HasSuffix(username, "@hotmail.com"):
			host, port = "smtp-mail.outlook.com", 587
		case strings.HasSuffix(username, "@yahoo.com"):
			host, port = "smtp.mail.yahoo.com", 587
		}
	}
	return &EmailNotifier{
		SMTPHost:     host,
		SMTPPort:     port,
		SMTPUsername: username,
		AppPassword:  password,
		TargetEmail:  receiver,
	}
}

// A bounded mail queue avoids accumulating goroutines when SMTP is slow.
var emailSlots = make(chan struct{}, 2)

func (n *EmailNotifier) SendEmail(subject, body string) {
	select {
	case emailSlots <- struct{}{}:
		go func() {
			defer func() { <-emailSlots }()
			if err := n.deliver(subject, body); err != nil {
				log.Printf("[SMTP] delivery failed: %v", err)
			}
		}()
	default: // Drop alerts rather than exhausting container memory.
	}
}

// buildMessage renders a plain-text RFC 822 message.
func (n *EmailNotifier) buildMessage(subject, body string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", n.SMTPUsername))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", n.TargetEmail))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	sb.WriteString("\r\n")
	sb.WriteString(body)
	sb.WriteString("\r\n")
	return sb.String()
}

// deliver performs a plaintext dial, upgrades with STARTTLS (port 587 style),
// authenticates and transmits the message.
func (n *EmailNotifier) deliver(subject, body string) error {
	addr := net.JoinHostPort(n.SMTPHost, fmt.Sprintf("%d", n.SMTPPort))

	connection, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		return fmt.Errorf("smtp dial failed: %w", err)
	}
	_ = connection.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(connection, n.SMTPHost)
	if err != nil {
		connection.Close()
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		cfg := &tls.Config{ServerName: n.SMTPHost, InsecureSkipVerify: false}
		if err := client.StartTLS(cfg); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

	if ok, _ := client.Extension("AUTH"); ok {
		if err := client.Auth(smtp.PlainAuth("", n.SMTPUsername, n.AppPassword, n.SMTPHost)); err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}
	}

	if err := client.Mail(n.SMTPUsername); err != nil {
		return fmt.Errorf("MAIL FROM failed: %w", err)
	}
	if err := client.Rcpt(n.TargetEmail); err != nil {
		return fmt.Errorf("RCPT TO failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(n.buildMessage(subject, body))); err != nil {
		return fmt.Errorf("writing body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing DATA failed: %w", err)
	}

	client.Quit()
	return nil
}

// withFallback kept for interface compatibility with future relay overrides.
var _ net.Conn // silence unused import if TLS dial path is removed later
