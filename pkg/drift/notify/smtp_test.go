package notify

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSMTP is a tiny SMTP server good enough to capture HELO/MAIL/RCPT/DATA.
type mockSMTP struct {
	listener net.Listener
	captured strings.Builder
	mu       sync.Mutex
}

func newMockSMTP(t *testing.T) *mockSMTP {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := &mockSMTP{listener: l}
	go m.serve()
	return m
}

func (m *mockSMTP) addr() string { return m.listener.Addr().String() }
func (m *mockSMTP) body() string  { m.mu.Lock(); defer m.mu.Unlock(); return m.captured.String() }

func (m *mockSMTP) serve() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			fmt.Fprintf(conn, "220 mock SMTP\r\n")
			buf := make([]byte, 4096)
			for {
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				line := string(buf[:n])
				m.mu.Lock()
				m.captured.WriteString(line)
				m.mu.Unlock()
				switch {
				case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
					fmt.Fprintf(conn, "250 OK\r\n")
				case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
					fmt.Fprintf(conn, "250 OK\r\n")
				case strings.HasPrefix(line, "DATA"):
					fmt.Fprintf(conn, "354 OK\r\n")
				case strings.HasPrefix(line, "QUIT"):
					fmt.Fprintf(conn, "221 OK\r\n")
					return
				case strings.HasSuffix(line, "\r\n.\r\n"):
					fmt.Fprintf(conn, "250 OK\r\n")
				}
			}
		}()
	}
}

func TestSMTP_SendsFormattedEmail(t *testing.T) {
	srv := newMockSMTP(t)
	defer srv.listener.Close()
	host, port, _ := net.SplitHostPort(srv.addr())
	notifier := &SMTPNotifier{
		Host: host, Port: port,
		From: "drift@nimbusfab.example", To: []string{"ops@nimbusfab.example"},
		Timeout: 2 * time.Second,
	}
	err := notifier.Notify(context.Background(), DriftEvent{
		Kind: "drift_detected", DeploymentID: "dep-1",
		ProjectName: "demo", Stack: "dev",
		DetectedAt: time.Now(),
		Targets: []DriftEventTarget{{ComponentName: "orders-db", Type: "database",
			Cloud: "aws", Region: "us-east-1", Summary: "+0 ~2 -0"}},
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	body := srv.body()
	if !strings.Contains(body, "orders-db") || !strings.Contains(body, "dep-1") {
		t.Errorf("expected email body to mention orders-db + dep-1; got: %s", body)
	}
	if !strings.Contains(body, "Subject: [nimbusfab] Drift") {
		t.Errorf("expected Subject header in email; got body:\n%s", body)
	}
}
