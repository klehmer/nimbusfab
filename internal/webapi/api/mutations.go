package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/klehmer/nimbusfab/internal/webapi/runner"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// Mutations groups POST handlers that kick off engine operations
// asynchronously. Each handler creates a broker publisher channel, hands
// it to the engine as the EventSink, and returns 202 immediately. The
// engine goroutine closes the publisher when the operation finishes,
// signaling any SSE subscribers.
type Mutations struct {
	Engine engine.Engine
	Broker *runner.RunBroker
	OrgID  string
}

type applyBody struct {
	PartialFailure string `json:"partialFailure,omitempty"` // "leave"|"rollback"|"retry-failed"
	AutoApprove    bool   `json:"autoApprove,omitempty"`
}

type destroyBody struct {
	AutoApprove bool `json:"autoApprove,omitempty"`
}

type driftBody struct{}

// PostApply → POST /api/v1/deployments/{id}/applies
func (m *Mutations) PostApply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body applyBody
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body is OK
	pub := m.Broker.Publisher(id)
	// Use a fresh context so the apply outlives the HTTP request handler
	// (which returns 202 immediately). r.Context() would be canceled when
	// the response is written.
	go func() {
		defer close(pub)
		_, _ = m.Engine.Apply(context.Background(), id, engine.ApplyOpts{
			AutoApprove:    body.AutoApprove,
			PartialFailure: provisioner.PartialFailurePolicy(body.PartialFailure),
			EventSink:      pub,
		})
	}()
	writeAccepted(w, id, "apply")
}

// PostDestroy → POST /api/v1/deployments/{id}/destroys
func (m *Mutations) PostDestroy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body destroyBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	pub := m.Broker.Publisher(id)
	go func() {
		defer close(pub)
		_, _ = m.Engine.Destroy(context.Background(), id, engine.DestroyOpts{
			AutoApprove: body.AutoApprove,
			EventSink:   pub,
		})
	}()
	writeAccepted(w, id, "destroy")
}

// PostDrift → POST /api/v1/deployments/{id}/drifts
func (m *Mutations) PostDrift(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body driftBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	pub := m.Broker.Publisher(id)
	go func() {
		defer close(pub)
		_, _ = m.Engine.DetectDrift(context.Background(), id, engine.DriftOpts{
			EventSink: pub,
		})
	}()
	writeAccepted(w, id, "drift")
}

func writeAccepted(w http.ResponseWriter, deploymentID, op string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"deploymentId": deploymentID,
			"operation":    op,
			"status":       "running",
			"eventsUrl":    "/api/v1/deployments/" + deploymentID + "/events",
		},
	})
}
