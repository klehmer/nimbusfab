package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/drift/notify"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// ---------------------------------------------------------------------------
// Fake repo plumbing
// ---------------------------------------------------------------------------

type fakeDeploymentRepo struct {
	deployments []inventory.Deployment
}

func (r *fakeDeploymentRepo) Get(_ context.Context, _, _ string) (*inventory.Deployment, error) {
	panic("fakeDeploymentRepo.Get not implemented")
}
func (r *fakeDeploymentRepo) Create(_ context.Context, _ inventory.Deployment) error {
	panic("fakeDeploymentRepo.Create not implemented")
}
func (r *fakeDeploymentRepo) UpdateStatus(_ context.Context, _, _, _ string, _ *time.Time) error {
	panic("fakeDeploymentRepo.UpdateStatus not implemented")
}
func (r *fakeDeploymentRepo) ListByProject(_ context.Context, _, _ string, _ int) ([]inventory.Deployment, error) {
	panic("fakeDeploymentRepo.ListByProject not implemented")
}
func (r *fakeDeploymentRepo) ListAll(_ context.Context, _ string) ([]inventory.Deployment, error) {
	return r.deployments, nil
}

type fakeDriftStatusRepo struct {
	rows []inventory.DriftRecord
}

func (r *fakeDriftStatusRepo) Get(_ context.Context, _, _ string) (*inventory.DriftRecord, error) {
	panic("fakeDriftStatusRepo.Get not implemented")
}
func (r *fakeDriftStatusRepo) Upsert(_ context.Context, _ inventory.DriftRecord) error {
	panic("fakeDriftStatusRepo.Upsert not implemented")
}
func (r *fakeDriftStatusRepo) ListByOrg(_ context.Context, _ string) ([]inventory.DriftRecord, error) {
	panic("fakeDriftStatusRepo.ListByOrg not implemented")
}
func (r *fakeDriftStatusRepo) LatestByDeployment(_ context.Context, _, _ string) ([]inventory.DriftRecord, error) {
	return r.rows, nil
}
func (r *fakeDriftStatusRepo) ListByProject(_ context.Context, _, _ string) ([]inventory.DriftRecord, error) {
	panic("fakeDriftStatusRepo.ListByProject not implemented")
}

// fakeRepo satisfies inventory.Repo. Only the methods the scheduler calls are
// implemented; all others panic so tests fail loudly on unexpected usage.
type fakeRepo struct {
	deps  *fakeDeploymentRepo
	drift *fakeDriftStatusRepo
}

func newFakeRepo(deps []inventory.Deployment, driftRows []inventory.DriftRecord) *fakeRepo {
	return &fakeRepo{
		deps:  &fakeDeploymentRepo{deployments: deps},
		drift: &fakeDriftStatusRepo{rows: driftRows},
	}
}

func (r *fakeRepo) Orgs() inventory.OrgRepo                        { panic("not implemented") }
func (r *fakeRepo) Users() inventory.UserRepo                      { panic("not implemented") }
func (r *fakeRepo) ApiTokens() inventory.ApiTokenRepo              { panic("not implemented") }
func (r *fakeRepo) Projects() inventory.ProjectRepo                { panic("not implemented") }
func (r *fakeRepo) Stacks() inventory.StackRepo                    { panic("not implemented") }
func (r *fakeRepo) Components() inventory.ComponentRepo            { panic("not implemented") }
func (r *fakeRepo) Compositions() inventory.CompositionRepo        { panic("not implemented") }
func (r *fakeRepo) Deployments() inventory.DeploymentRepo          { return r.deps }
func (r *fakeRepo) DeploymentTargets() inventory.DeploymentTargetRepo { panic("not implemented") }
func (r *fakeRepo) Runs() inventory.RunRepo                        { panic("not implemented") }
func (r *fakeRepo) RunLogs() inventory.RunLogRepo                  { panic("not implemented") }
func (r *fakeRepo) DriftStatus() inventory.DriftStatusRepo         { return r.drift }
func (r *fakeRepo) CostEstimates() inventory.CostEstimateRepo      { panic("not implemented") }
func (r *fakeRepo) CostActuals() inventory.CostActualRepo          { panic("not implemented") }
func (r *fakeRepo) SecretsRefs() inventory.SecretsRefRepo          { panic("not implemented") }
func (r *fakeRepo) AuditLog() inventory.AuditLogRepo               { panic("not implemented") }
func (r *fakeRepo) Migrate(_ context.Context) error                { return nil }
func (r *fakeRepo) Ping(_ context.Context) error                   { return nil }
func (r *fakeRepo) Close() error                                    { return nil }

// ---------------------------------------------------------------------------
// Fake engine
// ---------------------------------------------------------------------------

type fakeEngine struct {
	delay    time.Duration
	calls    atomic.Int64
	maxInFly atomic.Int64
	curInFly atomic.Int64
}

func (e *fakeEngine) DetectDrift(_ context.Context, _ string, _ engine.DriftOpts) (*engine.DriftReport, error) {
	e.calls.Add(1)
	cur := e.curInFly.Add(1)
	defer e.curInFly.Add(-1)
	// Track peak in-flight.
	for {
		old := e.maxInFly.Load()
		if cur <= old {
			break
		}
		if e.maxInFly.CompareAndSwap(old, cur) {
			break
		}
	}
	if e.delay > 0 {
		time.Sleep(e.delay)
	}
	return &engine.DriftReport{}, nil
}

// ---------------------------------------------------------------------------
// Safe notifier for transition event tests
// ---------------------------------------------------------------------------

type safeNotifier struct {
	mu     sync.Mutex
	events []notify.DriftEvent
}

func (n *safeNotifier) Notify(_ context.Context, ev notify.DriftEvent) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, ev)
	return nil
}

func (n *safeNotifier) Events() []notify.DriftEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make([]notify.DriftEvent, len(n.events))
	copy(cp, n.events)
	return cp
}

// ---------------------------------------------------------------------------
// Test 1: no deployments → no DetectDrift calls
// ---------------------------------------------------------------------------

func TestScheduler_NoDeployments_NoOp(t *testing.T) {
	eng := &fakeEngine{}
	repo := newFakeRepo(nil, nil)
	s := New(Config{
		OrgID:          "org1",
		GlobalInterval: 50 * time.Millisecond,
	}, repo, eng, notify.NopNotifier{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	if got := eng.calls.Load(); got != 0 {
		t.Errorf("expected 0 DetectDrift calls; got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 2: one applied deployment, no prior drift row → fires at least once
// ---------------------------------------------------------------------------

func TestScheduler_PastDueDeployment_Triggered(t *testing.T) {
	eng := &fakeEngine{}
	repo := newFakeRepo([]inventory.Deployment{
		{ID: "dep1", OrgID: "org1", Status: "succeeded"},
	}, nil)

	s := New(Config{
		OrgID:          "org1",
		GlobalInterval: 50 * time.Millisecond,
	}, repo, eng, notify.NopNotifier{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	if got := eng.calls.Load(); got < 1 {
		t.Errorf("expected at least 1 DetectDrift call; got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 3: per-deployment override of 1 hour should not fire repeatedly
// ---------------------------------------------------------------------------

func TestScheduler_OverrideBeatsGlobal(t *testing.T) {
	eng := &fakeEngine{}
	repo := newFakeRepo([]inventory.Deployment{
		{
			ID:                   "dep1",
			OrgID:                "org1",
			Status:               "succeeded",
			DriftIntervalSeconds: 3600, // 1-hour override
		},
	}, nil)

	s := New(Config{
		OrgID:          "org1",
		GlobalInterval: 50 * time.Millisecond,
	}, repo, eng, notify.NopNotifier{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	// The override is 1 hour; the boot-time tick fires once (lastChecked is
	// zero), then subsequent ticks skip it. We allow ≤1 call.
	if got := eng.calls.Load(); got > 1 {
		t.Errorf("expected at most 1 DetectDrift call (boot tick only); got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 4: concurrency cap — 5 deployments, MaxConcurrent=2
// ---------------------------------------------------------------------------

func TestScheduler_ConcurrencyCap(t *testing.T) {
	const numDeps = 5

	deps := make([]inventory.Deployment, numDeps)
	for i := range deps {
		deps[i] = inventory.Deployment{
			ID:     fmt.Sprintf("dep%d", i),
			OrgID:  "org1",
			Status: "succeeded",
		}
	}

	eng := &fakeEngine{delay: 30 * time.Millisecond}
	repo := newFakeRepo(deps, nil)

	s := New(Config{
		OrgID:          "org1",
		GlobalInterval: 50 * time.Millisecond,
		MaxConcurrent:  2,
	}, repo, eng, notify.NopNotifier{})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	s.Run(ctx)

	if peak := eng.maxInFly.Load(); peak > 2 {
		t.Errorf("concurrency exceeded cap: peak in-flight=%d, cap=2", peak)
	}
	if got := eng.calls.Load(); got < 1 {
		t.Errorf("expected at least 1 DetectDrift call; got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 5: context cancel exits cleanly
// ---------------------------------------------------------------------------

func TestScheduler_ContextCancel(t *testing.T) {
	eng := &fakeEngine{delay: 200 * time.Millisecond}
	repo := newFakeRepo([]inventory.Deployment{
		{ID: "dep1", OrgID: "org1", Status: "succeeded"},
	}, nil)

	s := New(Config{
		OrgID:          "org1",
		GlobalInterval: 50 * time.Millisecond,
	}, repo, eng, notify.NopNotifier{})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()

	// Let it start, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// clean exit
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Unit tests for transitionEvents
// ---------------------------------------------------------------------------

func TestTransitionEvents(t *testing.T) {
	dep := inventory.Deployment{ID: "dep1", ProjectID: "proj1"}

	type row struct {
		id       string
		hasDrift bool
	}
	makeRecord := func(r row) inventory.DriftRecord {
		return inventory.DriftRecord{
			DeploymentTargetID: r.id,
			DeploymentID:       dep.ID,
			HasDrift:           r.hasDrift,
		}
	}

	tests := []struct {
		name      string
		prior     []row
		curr      []row
		wantKinds []string
	}{
		{
			name:      "clean→drifted emits drift_detected",
			prior:     []row{{"t1", false}},
			curr:      []row{{"t1", true}},
			wantKinds: []string{"drift_detected"},
		},
		{
			name:      "drifted→clean emits drift_resolved",
			prior:     []row{{"t1", true}},
			curr:      []row{{"t1", false}},
			wantKinds: []string{"drift_resolved"},
		},
		{
			name:      "drifted→drifted no event",
			prior:     []row{{"t1", true}},
			curr:      []row{{"t1", true}},
			wantKinds: nil,
		},
		{
			name:      "clean→clean no event",
			prior:     []row{{"t1", false}},
			curr:      []row{{"t1", false}},
			wantKinds: nil,
		},
		{
			name:      "new target drifted emits drift_detected",
			prior:     nil,
			curr:      []row{{"t1", true}},
			wantKinds: []string{"drift_detected"},
		},
		{
			name:      "new target clean no event",
			prior:     nil,
			curr:      []row{{"t1", false}},
			wantKinds: nil,
		},
		{
			name:  "mixed: one resolved + one new drifted",
			prior: []row{{"t1", true}},
			curr: []row{
				{"t1", false}, // resolved
				{"t2", true},  // new, drifted
			},
			wantKinds: []string{"drift_detected", "drift_resolved"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			priorMap := make(map[string]inventory.DriftRecord)
			for _, r := range tc.prior {
				rec := makeRecord(r)
				priorMap[rec.DeploymentTargetID] = rec
			}
			var currRows []inventory.DriftRecord
			for _, r := range tc.curr {
				currRows = append(currRows, makeRecord(r))
			}

			events := transitionEvents(priorMap, currRows, dep, &engine.DriftReport{})

			if len(events) != len(tc.wantKinds) {
				t.Fatalf("expected %d events; got %d: %v", len(tc.wantKinds), len(events), events)
			}

			kindSet := make(map[string]bool)
			for _, e := range events {
				kindSet[e.Kind] = true
			}
			for _, k := range tc.wantKinds {
				if !kindSet[k] {
					t.Errorf("expected event kind %q not found in %v", k, events)
				}
			}

			for _, e := range events {
				if e.DeploymentID != dep.ID {
					t.Errorf("event.DeploymentID=%q; want %q", e.DeploymentID, dep.ID)
				}
			}
		})
	}
}
