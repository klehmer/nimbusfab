package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// FakeRunner records inputs and returns scripted outputs.
// All methods are concurrency-safe.
type FakeRunner struct {
	mu sync.Mutex

	InitCalls      []Workspace
	PlanCalls      []PlanCall
	ApplyCalls     []ApplyCall
	DestroyCalls   []Workspace
	ShowCalls      []ShowCall
	StateShowCalls []Workspace
	OutputCalls    []Workspace

	PlanReturn      *PlanArtifact
	PlanError       error
	ApplyError      error
	ShowReturn      []byte
	StateShowReturn []byte
	OutputReturn    map[string]any
	VersionReturn   string

	// DriftPlan, if non-nil, is returned from Plan when PlanOpts.RefreshOnly is true.
	DriftPlan *PlanArtifact

	// If non-empty, FakeRunner.Plan writes this byte slice to opts.OutFile so
	// downstream code that reads the plan file sees plausible content.
	PlanFileContents []byte

	// OnApply, if non-nil, is invoked synchronously inside Apply for tests that
	// need to materialize state files between calls. It runs after recording the
	// call and before returning ApplyError, so callers can write state files even
	// when the apply ultimately returns an error.
	OnApply func(ws Workspace, planFile string)
}

type PlanCall struct {
	Workspace Workspace
	Opts      PlanOpts
}

type ApplyCall struct {
	Workspace Workspace
	PlanFile  string
	Opts      ApplyOpts
}

type ShowCall struct {
	Workspace Workspace
	PlanFile  string
}

func NewFakeRunner() *FakeRunner {
	return &FakeRunner{
		VersionReturn: "OpenTofu v1.7.0",
		ShowReturn:    []byte(`{"format_version":"1.2"}`),
	}
}

func (f *FakeRunner) Init(ctx context.Context, ws Workspace) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InitCalls = append(f.InitCalls, ws)
	return nil
}

func (f *FakeRunner) Validate(ctx context.Context, ws Workspace) (*ValidateResult, error) {
	return &ValidateResult{Valid: true}, nil
}

func (f *FakeRunner) Plan(ctx context.Context, ws Workspace, opts PlanOpts) (*PlanArtifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PlanCalls = append(f.PlanCalls, PlanCall{Workspace: ws, Opts: opts})
	if f.PlanError != nil {
		return nil, f.PlanError
	}
	if opts.OutFile != "" && len(f.PlanFileContents) > 0 {
		if err := os.WriteFile(opts.OutFile, f.PlanFileContents, 0o600); err != nil {
			return nil, err
		}
	}
	if opts.RefreshOnly && f.DriftPlan != nil {
		return f.DriftPlan, nil
	}
	if f.PlanReturn != nil {
		return f.PlanReturn, nil
	}
	return &PlanArtifact{
		PlanFile:   opts.OutFile,
		JSONPlan:   []byte(`{"resource_changes":[]}`),
		HasChanges: false,
	}, nil
}

func (f *FakeRunner) Apply(ctx context.Context, ws Workspace, planFile string, opts ApplyOpts) error {
	f.mu.Lock()
	f.ApplyCalls = append(f.ApplyCalls, ApplyCall{Workspace: ws, PlanFile: planFile, Opts: opts})
	onApply := f.OnApply
	f.mu.Unlock()
	if onApply != nil {
		onApply(ws, planFile)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ApplyError
}

func (f *FakeRunner) Destroy(ctx context.Context, ws Workspace, opts DestroyOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DestroyCalls = append(f.DestroyCalls, ws)
	return nil
}

func (f *FakeRunner) Show(ctx context.Context, ws Workspace, planFile string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ShowCalls = append(f.ShowCalls, ShowCall{Workspace: ws, PlanFile: planFile})
	return f.ShowReturn, nil
}

func (f *FakeRunner) StateShow(ctx context.Context, ws Workspace) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StateShowCalls = append(f.StateShowCalls, ws)
	return f.StateShowReturn, nil
}

func (f *FakeRunner) StateRm(ctx context.Context, ws Workspace, address string) error  { return nil }
func (f *FakeRunner) StateMv(ctx context.Context, ws Workspace, from, to string) error { return nil }

func (f *FakeRunner) Output(ctx context.Context, ws Workspace) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.OutputCalls = append(f.OutputCalls, ws)
	return f.OutputReturn, nil
}

func (f *FakeRunner) Version(ctx context.Context) (string, error) {
	return f.VersionReturn, nil
}

// MarshalJSONPlan is a helper for tests: build a fake `tofu show -json plan`
// payload with N planned resource creates.
func (f *FakeRunner) MarshalJSONPlan(creates int) []byte {
	rcs := make([]map[string]any, 0, creates)
	for i := 0; i < creates; i++ {
		rcs = append(rcs, map[string]any{
			"address": fmt.Sprintf("aws_vpc.example_%d", i),
			"change":  map[string]any{"actions": []string{"create"}},
		})
	}
	out, _ := json.Marshal(map[string]any{"resource_changes": rcs})
	return out
}

// Compile-time check that FakeRunner satisfies Runner.
var _ Runner = (*FakeRunner)(nil)
