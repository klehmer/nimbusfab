package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExecRunner shells out to the `tofu` binary on $PATH (or the configured path).
type ExecRunner struct {
	Binary string
}

// NewExecRunner returns an ExecRunner using `tofu` on $PATH.
func NewExecRunner() *ExecRunner {
	return &ExecRunner{Binary: "tofu"}
}

func (e *ExecRunner) bin() string {
	if e.Binary != "" {
		return e.Binary
	}
	return "tofu"
}

func (e *ExecRunner) run(ctx context.Context, ws Workspace, args ...string) error {
	cmd := exec.CommandContext(ctx, e.bin(), args...)
	cmd.Dir = ws.Dir
	cmd.Env = mergeEnv(os.Environ(), ws.Environment)
	if ws.Stdout != nil {
		cmd.Stdout = ws.Stdout
	}
	if ws.Stderr != nil {
		cmd.Stderr = ws.Stderr
	}
	return cmd.Run()
}

func mergeEnv(base []string, extra map[string]string) []string {
	out := append([]string{}, base...)
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

func (e *ExecRunner) Init(ctx context.Context, ws Workspace) error {
	return e.run(ctx, ws, "init", "-no-color", "-input=false", "-lock-timeout=300s")
}

func (e *ExecRunner) Validate(ctx context.Context, ws Workspace) (*ValidateResult, error) {
	cmd := exec.CommandContext(ctx, e.bin(), "validate", "-json", "-no-color")
	cmd.Dir = ws.Dir
	cmd.Env = mergeEnv(os.Environ(), ws.Environment)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var raw struct {
		Valid       bool             `json:"valid"`
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if jerr := json.Unmarshal(out, &raw); jerr != nil {
		return nil, fmt.Errorf("tofu validate: malformed JSON output: %w", jerr)
	}
	res := &ValidateResult{Valid: raw.Valid}
	for _, d := range raw.Diagnostics {
		res.Diagnostics = append(res.Diagnostics, mapEvent(map[string]any{"type": "diagnostic", "diagnostic": d}))
	}
	return res, nil
}

func (e *ExecRunner) Plan(ctx context.Context, ws Workspace, opts PlanOpts) (*PlanArtifact, error) {
	if opts.OutFile == "" {
		return nil, fmt.Errorf("tofu Plan: PlanOpts.OutFile is required")
	}
	args := []string{"plan", "-no-color", "-input=false", "-json", "-lock-timeout=300s", "-out=" + opts.OutFile}
	if opts.Destroy {
		args = append(args, "-destroy")
	}
	if opts.Refresh {
		args = append(args, "-refresh=true")
	}
	if opts.RefreshOnly {
		args = append(args, "-refresh-only")
	}
	for k, v := range ws.Vars {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("tofu Plan: ws.Vars[%q] is %T; must be a pre-formatted HCL literal string", k, v)
		}
		args = append(args, "-var", k+"="+s)
	}
	if err := e.run(ctx, ws, args...); err != nil {
		return nil, err
	}
	showCmd := exec.CommandContext(ctx, e.bin(), "show", "-json", opts.OutFile)
	showCmd.Dir = ws.Dir
	jsonPlan, err := showCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tofu show -json: %w", err)
	}
	return &PlanArtifact{
		PlanFile:   opts.OutFile,
		JSONPlan:   jsonPlan,
		HasChanges: planHasChanges(jsonPlan),
	}, nil
}

func planHasChanges(jsonPlan []byte) bool {
	var p struct {
		ResourceChanges []map[string]any `json:"resource_changes"`
	}
	if err := json.Unmarshal(jsonPlan, &p); err != nil {
		return false
	}
	for _, rc := range p.ResourceChanges {
		change, _ := rc["change"].(map[string]any)
		actions, _ := change["actions"].([]any)
		for _, a := range actions {
			if s, _ := a.(string); s != "no-op" && s != "read" {
				return true
			}
		}
	}
	return false
}

func (e *ExecRunner) Apply(ctx context.Context, ws Workspace, planFile string, opts ApplyOpts) error {
	args := []string{"apply", "-no-color", "-input=false", "-json", "-lock-timeout=300s", planFile}
	return e.run(ctx, ws, args...)
}

func (e *ExecRunner) Destroy(ctx context.Context, ws Workspace, opts DestroyOpts) error {
	args := []string{"destroy", "-no-color", "-input=false", "-json", "-lock-timeout=300s"}
	if opts.AutoApprove {
		args = append(args, "-auto-approve")
	}
	for k, v := range ws.Vars {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("tofu Destroy: ws.Vars[%q] is %T; must be a pre-formatted HCL literal string", k, v)
		}
		args = append(args, "-var", k+"="+s)
	}
	return e.run(ctx, ws, args...)
}

func (e *ExecRunner) Show(ctx context.Context, ws Workspace, planFile string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.bin(), "show", "-json", planFile)
	cmd.Dir = ws.Dir
	return cmd.Output()
}

func (e *ExecRunner) StateShow(ctx context.Context, ws Workspace) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.bin(), "show", "-json")
	cmd.Dir = ws.Dir
	return cmd.Output()
}

func (e *ExecRunner) StateRm(ctx context.Context, ws Workspace, address string) error {
	return e.run(ctx, ws, "state", "rm", address)
}

func (e *ExecRunner) StateMv(ctx context.Context, ws Workspace, from, to string) error {
	return e.run(ctx, ws, "state", "mv", from, to)
}

func (e *ExecRunner) Output(ctx context.Context, ws Workspace) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, e.bin(), "output", "-json", "-no-color")
	cmd.Dir = ws.Dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if jerr := json.Unmarshal(out, &raw); jerr != nil {
		return nil, fmt.Errorf("tofu output -json: malformed JSON: %w", jerr)
	}
	res := map[string]any{}
	for k, v := range raw {
		if m, ok := v.(map[string]any); ok {
			res[k] = m["value"]
		} else {
			res[k] = v
		}
	}
	return res, nil
}

func (e *ExecRunner) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, e.bin(), "version", "-json")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var v struct {
		TerraformVersion string `json:"terraform_version"`
		Product          string `json:"product"`
	}
	if jerr := json.Unmarshal(out, &v); jerr == nil && v.TerraformVersion != "" {
		product := v.Product
		if product == "" {
			product = "OpenTofu"
		}
		return strings.TrimSpace(product) + " v" + v.TerraformVersion, nil
	}
	return strings.TrimSpace(string(out)), nil
}

// HasBinary reports whether the configured tofu binary is on $PATH.
// Used by integration tests to skip when tofu isn't installed.
func (e *ExecRunner) HasBinary() bool {
	_, err := exec.LookPath(e.bin())
	return err == nil
}

// readAll is a small helper used by tests to read a file. Lives here so the
// runner package has a single os import surface.
func readAll(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Compile-time check that ExecRunner satisfies Runner.
var _ Runner = (*ExecRunner)(nil)
