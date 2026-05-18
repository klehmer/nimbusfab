package notify

import (
	"os"
	"strings"
)

// FromEnv constructs a Notifier from environment variables:
//
//	NIMBUSFAB_DRIFT_WEBHOOK_URL — generic webhook destination.
//	NIMBUSFAB_DRIFT_SLACK_URL   — Slack incoming-webhook URL.
//	NIMBUSFAB_SMTP_HOST/_PORT/_USER/_PASS/_FROM and NIMBUSFAB_DRIFT_EMAIL_TO
//	                            — SMTP email destination.
//
// Returns NopNotifier when nothing is configured.
func FromEnv() Notifier {
	var ns MultiNotifier
	if u := os.Getenv("NIMBUSFAB_DRIFT_WEBHOOK_URL"); u != "" {
		ns = append(ns, &WebhookNotifier{URL: u})
	}
	if u := os.Getenv("NIMBUSFAB_DRIFT_SLACK_URL"); u != "" {
		ns = append(ns, &SlackNotifier{URL: u})
	}
	if h := os.Getenv("NIMBUSFAB_SMTP_HOST"); h != "" {
		to := os.Getenv("NIMBUSFAB_DRIFT_EMAIL_TO")
		var recipients []string
		for _, p := range strings.Split(to, ",") {
			if p = strings.TrimSpace(p); p != "" {
				recipients = append(recipients, p)
			}
		}
		if len(recipients) > 0 {
			ns = append(ns, &SMTPNotifier{
				Host: h, Port: os.Getenv("NIMBUSFAB_SMTP_PORT"),
				User: os.Getenv("NIMBUSFAB_SMTP_USER"),
				Pass: os.Getenv("NIMBUSFAB_SMTP_PASS"),
				From: os.Getenv("NIMBUSFAB_SMTP_FROM"),
				To:   recipients,
			})
		}
	}
	if len(ns) == 0 {
		return NopNotifier{}
	}
	return ns
}
