package main

import (
	"bytes"
	"strings"
	"testing"
)

// Exercises the full Phase 1 pipeline: multi-file project layout, loader
// discovery, validator phases 1-3, CLI exit code. The fixture lives in the
// loader package's testdata to avoid duplicating files.
func TestValidate_MultiFileFixture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{"../../internal/dsl/loader/testdata/multi-file"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout missing OK: %s", stdout.String())
	}
}
