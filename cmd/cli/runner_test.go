package main

import (
	"testing"

	"github.com/klehmer/nimbusfab/internal/tofu"
)

func TestDefaultRunner_DefaultIsExec(t *testing.T) {
	orig := flagFakeRunner
	defer func() { flagFakeRunner = orig }()

	flagFakeRunner = false
	r := defaultRunner()
	if _, ok := r.(*tofu.FakeRunner); ok {
		t.Errorf("default runner should NOT be *tofu.FakeRunner")
	}
}

func TestDefaultRunner_FakeWhenFlagSet(t *testing.T) {
	orig := flagFakeRunner
	defer func() { flagFakeRunner = orig }()

	flagFakeRunner = true
	r := defaultRunner()
	fake, ok := r.(*tofu.FakeRunner)
	if !ok {
		t.Fatalf("with --fake-runner the runner must be *tofu.FakeRunner, got %T", r)
	}
	if fake.PlanReturn == nil {
		t.Fatal("FakeRunner.PlanReturn should be set so plans look like work")
	}
	if !fake.PlanReturn.HasChanges {
		t.Error("FakeRunner.PlanReturn.HasChanges should be true so plans report changes")
	}
	if len(fake.PlanFileContents) == 0 {
		t.Error("FakeRunner.PlanFileContents should be non-empty so plan files have content")
	}
}
