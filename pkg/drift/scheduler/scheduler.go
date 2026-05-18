// Package scheduler implements the periodic drift-detection loop.
//
// The Scheduler polls all deployments for an org at a configurable interval.
// Each deployment may override the global cadence via
// Deployment.DriftIntervalSeconds. When a target transitions between
// "clean" and "drifted" states a DriftEvent is emitted to the Notifier.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/klehmer/nimbusfab/pkg/drift/notify"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// DriftEngine is the narrow contract the scheduler requires. It avoids a
// dependency on the full engine.Engine interface, which makes testing simpler.
type DriftEngine interface {
	DetectDrift(ctx context.Context, deploymentID string, opts engine.DriftOpts) (*engine.DriftReport, error)
}

// Config holds all tunable parameters for the Scheduler.
type Config struct {
	// OrgID is the tenant whose deployments are polled.
	OrgID string

	// GlobalInterval is the default drift poll cadence. Per-deployment
	// DriftIntervalSeconds overrides this when set.
	GlobalInterval time.Duration

	// MaxConcurrent is the maximum number of concurrent drift checks.
	// Defaults to 4 when zero.
	MaxConcurrent int
}

// Scheduler is the top-level drift polling daemon.
type Scheduler struct {
	cfg      Config
	repo     inventory.Repo
	engine   DriftEngine
	notifier notify.Notifier

	// sem is a global semaphore capping total concurrent drift-checks across
	// all in-flight ticks.
	sem chan struct{}

	// mu protects lastChecked — accessed from multiple goroutines.
	mu          sync.Mutex
	lastChecked map[string]time.Time // deployment ID → last detection start
}

// New constructs a Scheduler. Callers must call Run to start the loop.
func New(cfg Config, repo inventory.Repo, eng DriftEngine, notifier notify.Notifier) *Scheduler {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.GlobalInterval <= 0 {
		cfg.GlobalInterval = time.Hour
	}
	return &Scheduler{
		cfg:         cfg,
		repo:        repo,
		engine:      eng,
		notifier:    notifier,
		sem:         make(chan struct{}, cfg.MaxConcurrent),
		lastChecked: make(map[string]time.Time),
	}
}

// Run blocks until ctx is cancelled. It fires an immediate tick at startup,
// then on each ticker fire. The ticker interval is min(globalInterval, 60s)
// so the scheduler stays responsive to short per-deployment overrides without
// sleeping longer than necessary.
//
// Each tick is launched in its own goroutine so that a slow detection run
// does not delay the event loop. Run returns promptly when ctx is cancelled;
// in-flight detection goroutines may continue briefly but they check ctx
// themselves before doing meaningful work.
func (s *Scheduler) Run(ctx context.Context) {
	tickInterval := s.cfg.GlobalInterval
	if tickInterval > 60*time.Second {
		tickInterval = 60 * time.Second
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	// immediate tick at boot — launched as goroutine so Run stays responsive.
	go s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go s.tick(ctx)
		}
	}
}

// tick enumerates all deployments and spawns goroutines for past-due ones.
// The global semaphore s.sem caps total concurrent drift-checks across all
// concurrent tick goroutines.
func (s *Scheduler) tick(ctx context.Context) {
	deployments, err := s.repo.Deployments().ListAll(ctx, s.cfg.OrgID)
	if err != nil {
		slog.Warn("drift/scheduler: ListAll failed", "err", err)
		return
	}

	var wg sync.WaitGroup

	now := time.Now()

loop:
	for _, d := range deployments {
		d := d // capture loop var

		if !isApplied(d) {
			continue
		}

		effective := s.effectiveInterval(d)
		s.mu.Lock()
		last := s.lastChecked[d.ID]
		s.mu.Unlock()

		if !last.IsZero() && now.Sub(last) < effective {
			continue // not yet due
		}

		// Acquire a slot from the global semaphore before launching.
		// Block here (in this tick goroutine) until a slot is free.
		select {
		case s.sem <- struct{}{}: // slot acquired
		case <-ctx.Done():
			break loop
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-s.sem }() // release slot
			s.driftOne(ctx, d)
		}()
	}

	wg.Wait()
}

// isApplied returns true when the deployment has a terminal "succeeded" or
// "partial_failure" status — i.e., infra has been provisioned.
func isApplied(d inventory.Deployment) bool {
	return d.Status == "succeeded" || d.Status == "partial_failure"
}

// effectiveInterval resolves the cadence for a single deployment.
// A non-zero DriftIntervalSeconds on the deployment beats the global setting.
func (s *Scheduler) effectiveInterval(d inventory.Deployment) time.Duration {
	if d.DriftIntervalSeconds > 0 {
		return time.Duration(d.DriftIntervalSeconds) * time.Second
	}
	return s.cfg.GlobalInterval
}

// driftOne runs drift detection for one deployment and emits transition events.
func (s *Scheduler) driftOne(ctx context.Context, d inventory.Deployment) {
	// Snapshot prior state before detection.
	priorRows, err := s.repo.DriftStatus().LatestByDeployment(ctx, s.cfg.OrgID, d.ID)
	if err != nil {
		slog.Warn("drift/scheduler: LatestByDeployment (prior) failed",
			"deployment", d.ID, "err", err)
		return
	}
	priorMap := indexDriftRecords(priorRows)

	// Record the start time so the due-check uses a consistent timestamp
	// regardless of how long the detection takes.
	s.mu.Lock()
	s.lastChecked[d.ID] = time.Now()
	s.mu.Unlock()

	// Run detection.
	report, err := s.engine.DetectDrift(ctx, d.ID, engine.DriftOpts{})
	if err != nil {
		slog.Warn("drift/scheduler: DetectDrift failed",
			"deployment", d.ID, "err", err)
		return
	}
	if report == nil {
		return
	}

	// Snapshot current state after detection.
	currRows, err := s.repo.DriftStatus().LatestByDeployment(ctx, s.cfg.OrgID, d.ID)
	if err != nil {
		slog.Warn("drift/scheduler: LatestByDeployment (curr) failed",
			"deployment", d.ID, "err", err)
		return
	}

	// Emit events for any edge transitions.
	events := transitionEvents(priorMap, currRows, d, report)
	for _, ev := range events {
		if err := s.notifier.Notify(ctx, ev); err != nil {
			slog.Warn("drift/scheduler: notify failed",
				"deployment", d.ID, "kind", ev.Kind, "err", err)
		}
	}
}

// indexDriftRecords builds a map keyed by DeploymentTargetID.
func indexDriftRecords(rows []inventory.DriftRecord) map[string]inventory.DriftRecord {
	m := make(map[string]inventory.DriftRecord, len(rows))
	for _, r := range rows {
		m[r.DeploymentTargetID] = r
	}
	return m
}

// transitionEvents computes drift-edge events by comparing the prior snapshot
// of drift_status rows with the current snapshot.
//
// Rules:
//   - clean → drifted : emits "drift_detected"
//   - drifted → clean : emits "drift_resolved"
//   - no prior row + drifted : emits "drift_detected" (new target first seen drifted)
//   - drifted → drifted : no event (already known)
//   - clean → clean   : no event
//
// At most one event per kind is emitted per tick; all targets that share the
// same transition are aggregated into that event's Targets slice.
func transitionEvents(
	priorMap map[string]inventory.DriftRecord,
	currRows []inventory.DriftRecord,
	d inventory.Deployment,
	report *engine.DriftReport,
) []notify.DriftEvent {
	var detected, resolved []notify.DriftEventTarget

	// Build a quick lookup from the engine report for target summaries.
	summaryByTargetID := make(map[string]string)
	if report != nil {
		for _, tr := range report.TargetReports {
			summaryByTargetID[tr.DeploymentTargetID] = driftSummary(tr)
		}
	}

	for _, curr := range currRows {
		prior, hasPrior := priorMap[curr.DeploymentTargetID]

		target := notify.DriftEventTarget{
			ComponentName:      curr.ComponentName,
			Cloud:              curr.Cloud,
			Region:             curr.Region,
			DeploymentTargetID: curr.DeploymentTargetID,
			Summary:            summaryByTargetID[curr.DeploymentTargetID],
		}

		switch {
		case curr.HasDrift && (!hasPrior || !prior.HasDrift):
			// clean → drifted  OR  new target seen drifted for first time
			detected = append(detected, target)
		case !curr.HasDrift && hasPrior && prior.HasDrift:
			// drifted → clean
			resolved = append(resolved, target)
		}
	}

	var events []notify.DriftEvent
	now := time.Now()

	if len(detected) > 0 {
		events = append(events, notify.DriftEvent{
			Kind:         "drift_detected",
			DeploymentID: d.ID,
			ProjectID:    d.ProjectID,
			DetectedAt:   now,
			Targets:      detected,
		})
	}
	if len(resolved) > 0 {
		events = append(events, notify.DriftEvent{
			Kind:         "drift_resolved",
			DeploymentID: d.ID,
			ProjectID:    d.ProjectID,
			DetectedAt:   now,
			Targets:      resolved,
		})
	}
	return events
}

// driftSummary formats a human-readable diff summary for a target report.
func driftSummary(tr engine.TargetDriftReport) string {
	if !tr.HasDrift {
		return "no drift"
	}
	return fmt.Sprintf("+%d ~%d -%d",
		len(tr.Discovered), len(tr.Drifted), len(tr.Gone))
}

