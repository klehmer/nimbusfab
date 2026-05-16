package provisioner

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubBackend lets tests inject canned Resolve outputs.
type stubBackend struct {
	payload map[string]any
	err     error
}

func (stubBackend) Kind() string { return "stub" }
func (b stubBackend) Resolve(ctx context.Context, ref string) (map[string]any, error) {
	return b.payload, b.err
}

func TestResolveEnvFor_NilBackend(t *testing.T) {
	env, err := resolveEnvFor(context.Background(), nil, "aws-dev")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(env) != 0 {
		t.Errorf("env = %v, want empty", env)
	}
}

func TestResolveEnvFor_EmptyRef(t *testing.T) {
	env, err := resolveEnvFor(context.Background(), stubBackend{payload: map[string]any{"x": "y"}}, "")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(env) != 0 {
		t.Errorf("env = %v, want empty (empty ref short-circuits)", env)
	}
}

func TestResolveEnvFor_Resolves(t *testing.T) {
	env, err := resolveEnvFor(context.Background(), stubBackend{
		payload: map[string]any{"AWS_ACCESS_KEY_ID": "AKIA", "AWS_SECRET_ACCESS_KEY": "shh"},
	}, "aws-dev")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if env["AWS_ACCESS_KEY_ID"] != "AKIA" || env["AWS_SECRET_ACCESS_KEY"] != "shh" {
		t.Errorf("env = %v", env)
	}
}

func TestResolveEnvFor_PayloadHasNonStringValue(t *testing.T) {
	_, err := resolveEnvFor(context.Background(), stubBackend{
		payload: map[string]any{"AWS_ACCESS_KEY_ID": 42},
	}, "aws-dev")
	if err == nil {
		t.Fatal("expected error for non-string payload value")
	}
	if !strings.Contains(err.Error(), "non-string") {
		t.Errorf("err message should mention non-string: %v", err)
	}
}

func TestResolveEnvFor_BackendError(t *testing.T) {
	stubErr := errors.New("boom")
	_, err := resolveEnvFor(context.Background(), stubBackend{err: stubErr}, "aws-dev")
	if err == nil {
		t.Fatal("expected error from backend")
	}
	if !strings.Contains(err.Error(), "ErrSecretsRefUnresolved") {
		t.Errorf("err message should include ErrSecretsRefUnresolved: %v", err)
	}
}

func TestResolveEnvFor_NoBackendResolved(t *testing.T) {
	// Backend returns nil payload + nil error → Chain-style "not found".
	_, err := resolveEnvFor(context.Background(), stubBackend{}, "aws-dev")
	if err == nil {
		t.Fatal("expected ErrSecretsRefUnresolved")
	}
	if !strings.Contains(err.Error(), "ErrSecretsRefUnresolved") {
		t.Errorf("err message should include ErrSecretsRefUnresolved: %v", err)
	}
	if !strings.Contains(err.Error(), "aws-dev") {
		t.Errorf("err message should name the ref: %v", err)
	}
}
