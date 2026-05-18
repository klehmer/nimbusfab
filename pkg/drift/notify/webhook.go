package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookNotifier POSTs DriftEvents as application/json to URL. One retry
// on 5xx or transport error (first failure → wait Backoff, retry once;
// 4xx aborts immediately).
type WebhookNotifier struct {
	URL     string
	Client  *http.Client
	Backoff time.Duration
}

func (w *WebhookNotifier) Notify(ctx context.Context, event DriftEvent) error {
	if w.URL == "" {
		return nil
	}
	client := w.Client
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("webhook %s: %w", w.URL, err)
			}
			select {
			case <-time.After(w.backoffFor(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return fmt.Errorf("webhook %s: %d %s (abort, no retry)", w.URL, resp.StatusCode, resp.Status)
		case resp.StatusCode >= 500:
			if attempt == 2 {
				return fmt.Errorf("webhook %s: %d %s after retry", w.URL, resp.StatusCode, resp.Status)
			}
			select {
			case <-time.After(w.backoffFor(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (w *WebhookNotifier) backoffFor(attempt int) time.Duration {
	if w.Backoff == 0 {
		w.Backoff = time.Second
	}
	if attempt == 1 {
		return w.Backoff
	}
	return w.Backoff * 4
}
