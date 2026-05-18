package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestGraphCommand_NetworkOnly(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runGraph(graphArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Direction:   "tb",
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if code != 0 {
		t.Fatalf("exit code %d; stderr:\n%s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), `<svg`) {
		t.Errorf("stdout should start with <svg; got:\n%s", stdout.String()[:200])
	}
}

func TestGraphCommand_CrossRegionExits3(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runGraph(graphArgs{
		ProjectPath: "testdata/cross-region-project",
		Stack:       "dev",
		Direction:   "tb",
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if code != 3 {
		t.Errorf("expected exit 3 for cross-region; got %d (stderr: %s)", code, stderr.String())
	}
	// SVG should still be written so users can see the problem visually.
	if !strings.Contains(stdout.String(), `<svg`) {
		t.Errorf("stdout should still contain <svg on pairing failure; got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "cross-target") && !strings.Contains(stderr.String(), "no upstream target") && !strings.Contains(stderr.String(), "pairing") {
		t.Errorf("stderr should mention cross-target failure; got: %s", stderr.String())
	}
}

func TestGraphCommand_OutFlagWritesFile(t *testing.T) {
	outPath := t.TempDir() + "/graph.svg"
	stderr := &bytes.Buffer{}
	code := runGraph(graphArgs{
		ProjectPath: "testdata/network-only-project",
		Stack:       "dev",
		Direction:   "lr",
		OutPath:     outPath,
		Stderr:      stderr,
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out file: %v", err)
	}
	if !strings.HasPrefix(string(data), `<svg`) {
		t.Errorf("out file should be SVG; got: %s", string(data)[:200])
	}
}
