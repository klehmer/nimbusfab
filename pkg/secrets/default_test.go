package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultBackend_KindIsChain(t *testing.T) {
	if DefaultBackend().Kind() != "chain" {
		t.Errorf("Kind = %q, want chain", DefaultBackend().Kind())
	}
}

// chainWith returns a Chain whose file backend is rooted at the supplied
// directory; lets tests skip os.UserHomeDir resolution.
func chainWith(fileDir string) Backend {
	return NewChain(&EnvBackend{}, &FileBackend{Dir: fileDir})
}

func TestDefaultBackend_EnvFirstWins(t *testing.T) {
	dir := t.TempDir()
	// File would resolve to {"key":"file"}
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(`{"key":"file"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Env resolves to {"key":"env"}
	t.Setenv("NIMBUSFAB_SECRET_DEMO", `{"key":"env"}`)

	got, err := chainWith(dir).Resolve(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got["key"] != "env" {
		t.Errorf("Chain returned %v, want env (env should win over file)", got)
	}
}

func TestDefaultBackend_FileFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(`{"src":"file"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Env unset for "demo"
	t.Setenv("NIMBUSFAB_SECRET_DEMO", "")

	got, err := chainWith(dir).Resolve(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got["src"] != "file" {
		t.Errorf("Chain returned %v, want {src:file}", got)
	}
}

func TestDefaultBackend_NeitherReturnsNotFound(t *testing.T) {
	t.Setenv("NIMBUSFAB_SECRET_DEMO", "")
	dir := t.TempDir() // empty dir
	_, err := chainWith(dir).Resolve(context.Background(), "demo")
	var nf ErrNotFound
	if !errors.As(err, &nf) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
