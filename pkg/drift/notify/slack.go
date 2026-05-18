package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SlackNotifier POSTs DriftEvents to a Slack incoming-webhook URL,
// formatting the body with text + attachments. Reuses WebhookNotifier's
// retry / abort behavior internally.
type SlackNotifier struct {
	URL     string
	Client  *http.Client
	Backoff time.Duration
}

func (s *SlackNotifier) Notify(ctx context.Context, event DriftEvent) error {
	if s.URL == "" {
		return nil
	}
	body := buildSlackPayload(event)
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	jsonBody, _ := json.Marshal(body)
	// Reuse WebhookNotifier's POST + retry by delegating.
	w := &WebhookNotifier{URL: s.URL, Client: client, Backoff: s.Backoff}
	// We can't pass a body directly to WebhookNotifier's Notify(event); easier
	// to issue the request inline.
	for attempt := 1; attempt <= 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("slack %s: %w", s.URL, err)
			}
			select {
			case <-time.After(w.backoffFor(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return fmt.Errorf("slack %s: %d %s", s.URL, resp.StatusCode, resp.Status)
		}
		if attempt == 2 {
			return fmt.Errorf("slack %s: %d %s after retry", s.URL, resp.StatusCode, resp.Status)
		}
		select {
		case <-time.After(w.backoffFor(attempt)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func buildSlackPayload(event DriftEvent) map[string]any {
	color := "warning"
	verb := "detected"
	if event.Kind == "drift_resolved" {
		color = "good"
		verb = "resolved"
	}
	parts := []string{fmt.Sprintf("Drift %s in deployment %s", verb, event.DeploymentID)}
	for _, t := range event.Targets {
		parts = append(parts, fmt.Sprintf("• %s (%s) on %s/%s — %s",
			t.ComponentName, t.Type, t.Cloud, t.Region, t.Summary))
	}
	if event.DeploymentURL != "" {
		parts = append(parts, "<"+event.DeploymentURL+"|View deployment>")
	}
	return map[string]any{
		"text": strings.Join(parts, "\n"),
		"attachments": []map[string]any{
			{
				"color":    color,
				"fallback": fmt.Sprintf("Drift %s: %s", verb, event.DeploymentID),
				"fields": []map[string]any{
					{"title": "Project", "value": event.ProjectName, "short": true},
					{"title": "Stack", "value": event.Stack, "short": true},
				},
			},
		},
	}
}
