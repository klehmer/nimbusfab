package notify

import (
	"context"
	"testing"
)

func TestFromEnv_NoVars_ReturnsNop(t *testing.T) {
	t.Setenv("NIMBUSFAB_DRIFT_WEBHOOK_URL", "")
	t.Setenv("NIMBUSFAB_DRIFT_SLACK_URL", "")
	t.Setenv("NIMBUSFAB_SMTP_HOST", "")
	n := FromEnv()
	if _, ok := n.(NopNotifier); !ok {
		t.Errorf("expected NopNotifier when no env vars set; got %T", n)
	}
}

func TestFromEnv_AllConfigured(t *testing.T) {
	t.Setenv("NIMBUSFAB_DRIFT_WEBHOOK_URL", "http://example.com/hook")
	t.Setenv("NIMBUSFAB_DRIFT_SLACK_URL", "http://hooks.slack.com/x")
	t.Setenv("NIMBUSFAB_SMTP_HOST", "smtp.example.com")
	t.Setenv("NIMBUSFAB_SMTP_FROM", "drift@example.com")
	t.Setenv("NIMBUSFAB_DRIFT_EMAIL_TO", "ops@example.com,sre@example.com")
	n := FromEnv()
	m, ok := n.(MultiNotifier)
	if !ok {
		t.Fatalf("expected MultiNotifier; got %T", n)
	}
	if len(m) != 3 {
		t.Errorf("expected 3 notifiers; got %d", len(m))
	}
	// Smoke: calling Notify should not panic.
	_ = m.Notify(context.Background(), DriftEvent{Kind: "drift_detected"})
}
