package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhook_HappyPath(t *testing.T) {
	var got DriftEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	w := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Backoff: 10 * time.Millisecond}
	err := w.Notify(context.Background(), DriftEvent{Kind: "drift_detected", DeploymentID: "dep-1"})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got.Kind != "drift_detected" || got.DeploymentID != "dep-1" {
		t.Errorf("payload not received: %+v", got)
	}
}

func TestWebhook_Retries5xxThenSucceeds(t *testing.T) {
	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempt.Add(1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	w := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Backoff: 10 * time.Millisecond}
	if err := w.Notify(context.Background(), DriftEvent{Kind: "x"}); err != nil {
		t.Errorf("expected retry to succeed; got %v", err)
	}
	if attempt.Load() != 2 {
		t.Errorf("expected 2 attempts; got %d", attempt.Load())
	}
}

func TestWebhook_AbortOn4xx(t *testing.T) {
	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt.Add(1)
		w.WriteHeader(400)
	}))
	defer srv.Close()
	w := &WebhookNotifier{URL: srv.URL, Client: srv.Client(), Backoff: 10 * time.Millisecond}
	if err := w.Notify(context.Background(), DriftEvent{Kind: "x"}); err == nil {
		t.Error("expected 4xx to return error")
	}
	if attempt.Load() != 1 {
		t.Errorf("4xx should NOT retry; got %d attempts", attempt.Load())
	}
}
