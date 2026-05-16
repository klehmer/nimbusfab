package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFile_KindIsFile(t *testing.T) {
	if (&FileBackend{}).Kind() != "file" {
		t.Errorf("Kind = %q, want file", (&FileBackend{}).Kind())
	}
}

func TestFile_MissingReturnsNilNil(t *testing.T) {
	b := &FileBackend{Dir: t.TempDir()}
	got, err := b.Resolve(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aws-dev.json")
	if err := os.WriteFile(path, []byte(`{"AWS_ACCESS_KEY_ID":"AKIA","AWS_SECRET_ACCESS_KEY":"shh"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b := &FileBackend{Dir: dir}
	got, err := b.Resolve(context.Background(), "aws-dev")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got["AWS_ACCESS_KEY_ID"] != "AKIA" {
		t.Errorf("AWS_ACCESS_KEY_ID = %v", got["AWS_ACCESS_KEY_ID"])
	}
}

func TestFile_BadJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b := &FileBackend{Dir: dir}
	_, err := b.Resolve(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFile_EmptyRefReturnsNilNil(t *testing.T) {
	b := &FileBackend{Dir: t.TempDir()}
	got, err := b.Resolve(context.Background(), "")
	if err != nil || got != nil {
		t.Errorf("empty ref: got=%v err=%v, want nil/nil", got, err)
	}
}

func TestFile_EmptyDirReturnsNilNil(t *testing.T) {
	b := &FileBackend{Dir: ""}
	got, err := b.Resolve(context.Background(), "anything")
	if err != nil || got != nil {
		t.Errorf("empty dir: got=%v err=%v, want nil/nil", got, err)
	}
}

func TestNewFileBackend_DefaultsToHomeDir(t *testing.T) {
	b := NewFileBackend("")
	// Either resolved to <home>/.nimbusfab/secrets or empty if no home dir.
	if b.Dir != "" && filepath.Base(b.Dir) != "secrets" {
		t.Errorf("Dir = %q (expected to end with .nimbusfab/secrets)", b.Dir)
	}
}
