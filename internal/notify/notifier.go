package notify

import (
	"context"
	"log"
	"strings"
	"time"

	"floweysales/internal/mailer"
)

// EmailNotifier sends alert emails asynchronously. Rendering and SMTP delivery
// (STARTTLS) are done by the shared internal/mailer package.
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

// SendEmail queues an alert. The body is plain text.
func (n *EmailNotifier) SendEmail(subject, body string) {
	select {
	case emailSlots <- struct{}{}:
		go func() {
			defer func() { <-emailSlots }()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := mailer.Send(ctx, n.config(), n.TargetEmail, n.message(subject, body)); err != nil {
				log.Printf("[SMTP] delivery failed: %v", err)
			}
		}()
	default: // Drop alerts rather than exhausting container memory.
	}
}

func (n *EmailNotifier) config() mailer.Config {
	return mailer.Config{Host: n.SMTPHost, Port: n.SMTPPort, Username: n.SMTPUsername, Password: n.AppPassword, Sender: n.SMTPUsername}
}

func (n *EmailNotifier) message(subject, body string) mailer.Message {
	return mailer.Message{Kind: mailer.KindAlert, Subject: subject, Preheader: subject, ContentType: mailer.Text, Content: body}
}
