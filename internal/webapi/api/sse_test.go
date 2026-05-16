package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/api"
	"github.com/klehmer/nimbusfab/internal/webapi/runner"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// newSSEServer mounts the SSE handler at /events/{id} for the test.
func newSSEServer(t *testing.T, sse *api.SSEEvents) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events/{id}", sse.Handle)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// startSSEClient opens the SSE connection and returns a scanner ready to
// read lines. The associated cancel cleans up the request context.
func startSSEClient(t *testing.T, srv *httptest.Server, deploymentID string) (*bufio.Scanner, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events/"+deploymentID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("SSE GET: %v", err)
	}
	if resp.StatusCode != 200 {
		cancel()
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		cancel()
		t.Fatalf("Content-Type = %q", ct)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 8192), 64*1024)
	// Cleanup also closes the body when the test ends.
	t.Cleanup(func() {
		cancel()
		_ = resp.Body.Close()
	})
	return scanner, cancel
}

// readUntil reads SSE lines into a slice until needle appears in any line
// OR until timeout. Returns the lines collected so far on timeout.
func readUntil(t *testing.T, scanner *bufio.Scanner, needle string, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lines []string
	for time.Now().Before(deadline) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if scanner.Scan() {
				return
			}
		}()
		select {
		case <-done:
			line := scanner.Text()
			lines = append(lines, line)
			if needle == "" || strings.Contains(line, needle) {
				return lines
			}
		case <-time.After(time.Until(deadline)):
			return lines
		}
	}
	return lines
}

func TestSSE_StreamsEvents(t *testing.T) {
	broker := runner.NewRunBroker(16)
	sse := &api.SSEEvents{Broker: broker}
	srv := newSSEServer(t, sse)
	scanner, _ := startSSEClient(t, srv, "d-1")
	// First line should be the ": connected" hello comment.
	first := readUntil(t, scanner, "connected", time.Second)
	gotHello := false
	for _, l := range first {
		if strings.Contains(l, "connected") {
			gotHello = true
		}
	}
	if !gotHello {
		t.Errorf("missing connected hello: %v", first)
	}

	pub := broker.Publisher("d-1")
	pub <- provisioner.RunEvent{Kind: provisioner.RunEventStart, Message: "begin"}
	pub <- provisioner.RunEvent{Kind: provisioner.RunEventLog, Message: "hello"}
	pub <- provisioner.RunEvent{Kind: provisioner.RunEventSuccess, Message: "done"}
	close(pub)

	all := readUntil(t, scanner, "complete", 2*time.Second)
	body := strings.Join(all, "\n")
	for _, want := range []string{`"kind":"start"`, `"message":"begin"`, `"kind":"log"`, `"message":"hello"`, `"kind":"success"`, `"message":"done"`, "event: complete"} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestSSE_HeartbeatOnIdle(t *testing.T) {
	broker := runner.NewRunBroker(16)
	sse := &api.SSEEvents{Broker: broker, HeartbeatTick: 50 * time.Millisecond}
	srv := newSSEServer(t, sse)
	scanner, _ := startSSEClient(t, srv, "d-1")
	all := readUntil(t, scanner, "ping", time.Second)
	body := strings.Join(all, "\n")
	if !strings.Contains(body, "ping") {
		t.Errorf("expected ': ping' heartbeat within 1s; got:\n%s", body)
	}
}

func TestSSE_CompleteOnPublisherClose(t *testing.T) {
	broker := runner.NewRunBroker(16)
	sse := &api.SSEEvents{Broker: broker}
	srv := newSSEServer(t, sse)
	scanner, _ := startSSEClient(t, srv, "d-1")
	// Drain hello.
	_ = readUntil(t, scanner, "connected", time.Second)
	pub := broker.Publisher("d-1")
	close(pub)
	all := readUntil(t, scanner, "complete", time.Second)
	body := strings.Join(all, "\n")
	if !strings.Contains(body, "complete") {
		t.Errorf("missing complete event: %s", body)
	}
}

func TestSSE_ClientDisconnect(t *testing.T) {
	broker := runner.NewRunBroker(16)
	sse := &api.SSEEvents{Broker: broker}
	srv := newSSEServer(t, sse)
	_, cancel := startSSEClient(t, srv, "d-1")
	// Cancel client; handler should return cleanly (test would hang on
	// any goroutine leak; the cleanup-driven 30s test timeout would
	// catch that). No explicit assert beyond "doesn't hang".
	cancel()
	time.Sleep(50 * time.Millisecond)
}
