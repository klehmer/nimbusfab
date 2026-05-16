package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/api"
	"github.com/klehmer/nimbusfab/internal/webapi/runner"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// stubEngine records the calls + EventSinks it receives, emits one
// RunEvent of the requested kind, then returns. Implements engine.Engine.
type stubEngine struct {
	mu          sync.Mutex
	applyCalls  []string
	destroyCall []string
	driftCalls  []string
	gotSink     chan<- provisioner.RunEvent
	done        chan struct{}
}

func newStubEngine() *stubEngine { return &stubEngine{done: make(chan struct{}, 16)} }

func (s *stubEngine) Apply(ctx context.Context, planID string, opts engine.ApplyOpts) (string, error) {
	s.mu.Lock()
	s.applyCalls = append(s.applyCalls, planID)
	s.gotSink = opts.EventSink
	s.mu.Unlock()
	if opts.EventSink != nil {
		opts.EventSink <- provisioner.RunEvent{Kind: provisioner.RunEventStart, Message: "stub apply"}
	}
	s.done <- struct{}{}
	return planID, nil
}

func (s *stubEngine) Destroy(ctx context.Context, deploymentID string, opts engine.DestroyOpts) (string, error) {
	s.mu.Lock()
	s.destroyCall = append(s.destroyCall, deploymentID)
	s.mu.Unlock()
	if opts.EventSink != nil {
		opts.EventSink <- provisioner.RunEvent{Kind: provisioner.RunEventStart, Message: "stub destroy"}
	}
	s.done <- struct{}{}
	return deploymentID, nil
}

func (s *stubEngine) DetectDrift(ctx context.Context, deploymentID string, opts engine.DriftOpts) (*engine.DriftReport, error) {
	s.mu.Lock()
	s.driftCalls = append(s.driftCalls, deploymentID)
	s.mu.Unlock()
	if opts.EventSink != nil {
		opts.EventSink <- provisioner.RunEvent{Kind: provisioner.RunEventStart, Message: "stub drift"}
	}
	s.done <- struct{}{}
	return &engine.DriftReport{}, nil
}

// Remaining Engine methods are unused by the mutating-endpoint tests.
func (s *stubEngine) LoadProject(context.Context, string) (*ir.Project, error) { return nil, nil }
func (s *stubEngine) Validate(context.Context, *ir.Project) (*engine.ValidationReport, error) {
	return nil, nil
}
func (s *stubEngine) Plan(context.Context, *ir.Project, string, engine.PlanOpts) (*engine.PlanResult, error) {
	return nil, nil
}
func (s *stubEngine) Import(context.Context, *ir.Project, engine.ImportMap) (*engine.ImportResult, error) {
	return nil, nil
}
func (s *stubEngine) GetRun(context.Context, string) (*engine.Run, error) { return nil, nil }
func (s *stubEngine) StreamRun(context.Context, string) (<-chan engine.RunEvent, error) {
	return nil, nil
}
func (s *stubEngine) EstimateCost(context.Context, *engine.PlanResult) (*engine.CostEstimate, error) {
	return nil, nil
}
func (s *stubEngine) GetCostActuals(context.Context, engine.CostQuery) (*engine.CostReport, error) {
	return nil, nil
}
func (s *stubEngine) ApplyWithPlan(context.Context, *engine.PlanResult, engine.ApplyOpts) (*engine.ApplyResult, error) {
	return nil, nil
}
func (s *stubEngine) DestroyWithPlan(context.Context, *engine.PlanResult, engine.DestroyOpts) (*engine.ApplyResult, error) {
	return nil, nil
}
func (s *stubEngine) DetectDriftWithPlan(context.Context, *engine.PlanResult) (*engine.DriftReport, error) {
	return nil, nil
}

func waitDone(t *testing.T, s *stubEngine) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stub engine call never completed")
	}
}

func TestPostApply_Returns202(t *testing.T) {
	stub := newStubEngine()
	broker := runner.NewRunBroker(16)
	m := &api.Mutations{Engine: stub, Broker: broker, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/deployments/d-1/applies", strings.NewReader(`{}`))
	req.SetPathValue("id", "d-1")
	m.PostApply(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"deploymentId":"d-1"`) {
		t.Errorf("body missing deploymentId: %s", body)
	}
	if !strings.Contains(body, `"operation":"apply"`) {
		t.Errorf("body missing operation: %s", body)
	}
	waitDone(t, stub)
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.applyCalls) != 1 || stub.applyCalls[0] != "d-1" {
		t.Errorf("applyCalls = %v", stub.applyCalls)
	}
	if stub.gotSink == nil {
		t.Error("stub did not receive EventSink")
	}
}

func TestPostDestroy_Returns202(t *testing.T) {
	stub := newStubEngine()
	broker := runner.NewRunBroker(16)
	m := &api.Mutations{Engine: stub, Broker: broker, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/deployments/d-1/destroys", strings.NewReader(`{}`))
	req.SetPathValue("id", "d-1")
	m.PostDestroy(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d", rec.Code)
	}
	waitDone(t, stub)
	if len(stub.destroyCall) != 1 {
		t.Errorf("destroyCall = %v", stub.destroyCall)
	}
}

func TestPostDrift_Returns202(t *testing.T) {
	stub := newStubEngine()
	broker := runner.NewRunBroker(16)
	m := &api.Mutations{Engine: stub, Broker: broker, OrgID: "default"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/deployments/d-1/drifts", strings.NewReader(`{}`))
	req.SetPathValue("id", "d-1")
	m.PostDrift(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d", rec.Code)
	}
	waitDone(t, stub)
	if len(stub.driftCalls) != 1 {
		t.Errorf("driftCalls = %v", stub.driftCalls)
	}
}

func TestPostApply_EventReachesSubscriber(t *testing.T) {
	stub := newStubEngine()
	broker := runner.NewRunBroker(16)
	m := &api.Mutations{Engine: stub, Broker: broker, OrgID: "default"}
	// Subscribe BEFORE the POST so we don't miss the early event.
	ch, unsub := broker.Subscribe("d-1")
	defer unsub()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/deployments/d-1/applies", strings.NewReader(`{}`))
	req.SetPathValue("id", "d-1")
	m.PostApply(rec, req)

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before event arrived")
		}
		if evt.Message != "stub apply" {
			t.Errorf("evt.Message = %q", evt.Message)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no event reached subscriber within 500ms")
	}
}
