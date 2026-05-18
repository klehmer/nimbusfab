package notify

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"net/smtp"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/drift_email.txt
var emailTemplateFS embed.FS

// SMTPNotifier sends DriftEvents as plaintext emails via SMTP with PlainAuth.
// Subject + body are rendered from templates/drift_email.txt.
type SMTPNotifier struct {
	Host, Port string
	User, Pass string
	From       string
	To         []string
	Timeout    time.Duration
}

var emailTmpl = func() *template.Template {
	body, _ := emailTemplateFS.ReadFile("templates/drift_email.txt")
	t, _ := template.New("email").Funcs(template.FuncMap{
		"verbForKind": func(k string) string {
			if k == "drift_resolved" {
				return "resolved"
			}
			return "detected"
		},
	}).Parse(string(body))
	return t
}()

func (s *SMTPNotifier) Notify(ctx context.Context, event DriftEvent) error {
	if s.Host == "" || len(s.To) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := emailTmpl.Execute(&buf, event); err != nil {
		return fmt.Errorf("smtp template: %w", err)
	}
	// The template begins with "Subject: ..." so the raw bytes are
	// already in RFC-822 header-then-body shape.
	msg := buf.Bytes()
	// Prepend To/From headers expected by SMTP clients.
	full := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\n", s.From, strings.Join(s.To, ", ")))
	full = append(full, msg...)

	port := s.Port
	if port == "" {
		port = "587"
	}
	addr := s.Host + ":" + port
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	return smtp.SendMail(addr, auth, s.From, s.To, full)
}
