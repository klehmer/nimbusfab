package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlack_SendsSlackShape(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := &SlackNotifier{URL: srv.URL, Client: srv.Client()}
	err := n.Notify(context.Background(), DriftEvent{
		Kind: "drift_detected", DeploymentID: "dep-1", ProjectName: "demo",
		Targets: []DriftEventTarget{
			{ComponentName: "orders-db", Type: "database", Cloud: "aws", Region: "us-east-1", Summary: "+0 ~2 -0"},
		},
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, ok := payload["text"]; !ok {
		t.Errorf("Slack payload missing 'text' field: %+v", payload)
	}
	attachments, _ := payload["attachments"].([]any)
	if len(attachments) == 0 {
		t.Errorf("Slack payload missing attachments: %+v", payload)
	}
}
