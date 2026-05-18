// Package notify implements drift-event notifications. Three transports
// implement the Notifier interface — webhook, Slack, email — and a
// MultiNotifier fans events out to all configured transports. Failures of
// one transport do not block others.
package notify

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DriftEvent is the canonical payload for a drift transition. Both
// "drift_detected" (clean → drifted) and "drift_resolved" (drifted →
// clean) events share this shape.
type DriftEvent struct {
	Kind          string            // "drift_detected" | "drift_resolved"
	DeploymentID  string
	ProjectID     string
	ProjectName   string
	Stack         string
	DetectedAt    time.Time
	Targets       []DriftEventTarget
	DeploymentURL string // omitted (empty) when NIMBUSFAB_PUBLIC_URL unset
}

// DriftEventTarget is one target inside a DriftEvent.
type DriftEventTarget struct {
	ComponentName      string
	Type               string
	Cloud, Region      string
	Summary            string // "+0 ~2 -0"
	DeploymentTargetID string
}

// Notifier is the contract every drift transport implements.
type Notifier interface {
	Notify(ctx context.Context, event DriftEvent) error
}

// MultiNotifier fans an event out to every contained Notifier
// concurrently. Per-transport errors are logged at WARN; the outer
// Notify always returns nil.
type MultiNotifier []Notifier

func (m MultiNotifier) Notify(ctx context.Context, event DriftEvent) error {
	var wg sync.WaitGroup
	for _, n := range m {
		wg.Add(1)
		go func(n Notifier) {
			defer wg.Done()
			if err := n.Notify(ctx, event); err != nil {
				slog.Warn("notify transport failed", "err", err, "event", event.Kind)
			}
		}(n)
	}
	wg.Wait()
	return nil
}

// NopNotifier is the zero-config default — does nothing.
type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, DriftEvent) error { return nil }
