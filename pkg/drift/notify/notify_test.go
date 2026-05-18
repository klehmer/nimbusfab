package notify

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type recordingNotifier struct {
	calls atomic.Int32
	err   error
}

func (r *recordingNotifier) Notify(ctx context.Context, e DriftEvent) error {
	r.calls.Add(1)
	return r.err
}

func TestMultiNotifier_FanOut(t *testing.T) {
	a := &recordingNotifier{}
	b := &recordingNotifier{}
	m := MultiNotifier{a, b}
	if err := m.Notify(context.Background(), DriftEvent{Kind: "drift_detected"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Errorf("each notifier should be called once; got a=%d b=%d", a.calls.Load(), b.calls.Load())
	}
}

func TestMultiNotifier_OnePartialFailureDoesNotBlockOthers(t *testing.T) {
	a := &recordingNotifier{err: errors.New("boom")}
	b := &recordingNotifier{}
	m := MultiNotifier{a, b}
	if err := m.Notify(context.Background(), DriftEvent{Kind: "drift_detected"}); err != nil {
		t.Errorf("Multi should swallow per-transport errors; got %v", err)
	}
	if b.calls.Load() != 1 {
		t.Errorf("second notifier should still be called; got %d calls", b.calls.Load())
	}
}

func TestNopNotifier(t *testing.T) {
	if err := (NopNotifier{}).Notify(context.Background(), DriftEvent{Kind: "x", DetectedAt: time.Now()}); err != nil {
		t.Errorf("NopNotifier returned error: %v", err)
	}
}
