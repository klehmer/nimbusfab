package secrets

import (
	"context"
	"testing"
)

func TestEnv_KindIsEnv(t *testing.T) {
	if (&EnvBackend{}).Kind() != "env" {
		t.Errorf("Kind = %q, want env", (&EnvBackend{}).Kind())
	}
}

func TestEnv_UnsetReturnsNilNil(t *testing.T) {
	t.Setenv("NIMBUSFAB_SECRET_NOTHING", "")
	b := &EnvBackend{}
	got, err := b.Resolve(context.Background(), "nothing")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestEnv_HappyPath(t *testing.T) {
	t.Setenv("NIMBUSFAB_SECRET_AWS_DEV", `{"AWS_ACCESS_KEY_ID":"AKIA","AWS_SECRET_ACCESS_KEY":"shh"}`)
	b := &EnvBackend{}
	got, err := b.Resolve(context.Background(), "aws-dev")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got["AWS_ACCESS_KEY_ID"] != "AKIA" {
		t.Errorf("AWS_ACCESS_KEY_ID = %v", got["AWS_ACCESS_KEY_ID"])
	}
	if got["AWS_SECRET_ACCESS_KEY"] != "shh" {
		t.Errorf("AWS_SECRET_ACCESS_KEY = %v", got["AWS_SECRET_ACCESS_KEY"])
	}
}

func TestEnv_BadJSONReturnsError(t *testing.T) {
	t.Setenv("NIMBUSFAB_SECRET_BAD", "not json")
	_, err := (&EnvBackend{}).Resolve(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEnv_EmptyRefReturnsNilNil(t *testing.T) {
	got, err := (&EnvBackend{}).Resolve(context.Background(), "")
	if err != nil || got != nil {
		t.Errorf("empty ref: got=%v err=%v, want nil/nil", got, err)
	}
}

func TestEnv_RefMapping(t *testing.T) {
	// "aws-dev" → NIMBUSFAB_SECRET_AWS_DEV (uppercase + hyphen→underscore)
	t.Setenv("NIMBUSFAB_SECRET_AWS_DEV", `{"k":"v"}`)
	got, _ := (&EnvBackend{}).Resolve(context.Background(), "aws-dev")
	if got == nil || got["k"] != "v" {
		t.Errorf("aws-dev: got %v, want {k:v}", got)
	}
}
