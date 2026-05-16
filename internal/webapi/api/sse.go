package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/runner"
)

// SSEEvents handles GET /api/v1/deployments/{id}/events. Subscribers
// receive RunEvents published AFTER they connect; no replay. Connection
// closes when the publisher channel closes (operation done) OR the client
// disconnects.
type SSEEvents struct {
	Broker        *runner.RunBroker
	HeartbeatTick time.Duration // default 15s; tests override for fast assertion
}

const defaultHeartbeat = 15 * time.Second

// Handle implements the SSE handler.
func (s *SSEEvents) Handle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering

	ch, unsub := s.Broker.Subscribe(id)
	defer unsub()

	tick := s.HeartbeatTick
	if tick <= 0 {
		tick = defaultHeartbeat
	}
	heartbeat := time.NewTicker(tick)
	defer heartbeat.Stop()
	var eventID uint64

	// Write a hello comment so clients know the connection is open even
	// before any real event arrives.
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	f.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			f.Flush()
		case evt, ok := <-ch:
			if !ok {
				_, _ = fmt.Fprintf(w, "event: complete\ndata: {}\n\n")
				f.Flush()
				return
			}
			eventID++
			payload, _ := json.Marshal(map[string]any{
				"timestamp":          evt.Timestamp.Format(time.RFC3339),
				"deploymentTargetId": evt.DeploymentTargetID,
				"component":          evt.Component,
				"cloud":              evt.Cloud,
				"region":             evt.Region,
				"kind":               string(evt.Kind),
				"message":            evt.Message,
			})
			_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", eventID, evt.Kind, payload)
			f.Flush()
		}
	}
}
