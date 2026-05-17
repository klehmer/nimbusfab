//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// TestFullStack_TofuValidate exercises the workspace renderer against the
// full-stack-project fixture (4 components × 3 clouds = 12 targets). For
// each target it runs `tofu init` (cached per-target inside t.TempDir) and
// `tofu validate`. The test asserts the workspace JSON is parseable and
// passes tofu's schema check — catching adapter-side type mismatches,
// undefined-attribute bugs, and ref-name skew that pure-Go unit tests with
// FakeRunner cannot see.
//
// The test uses FakeRunner for the provisioner so the test does not depend
// on real `tofu plan` working end-to-end (cross-component planning order +
// state-path resolution is deferred to v1.1). FakeRunner returns success
// from Plan() so every workspace is materialized regardless of whether
// real-tofu plan would succeed.
//
// Cost: each `tofu init` downloads the relevant provider; with the per-test
// tempdir layout the cache is per-workspace, so ~12 provider downloads.
// Expect ~5 min cold, faster on warm cache. Gated by `-tags=integration`.
func TestFullStack_TofuValidate(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH; skipping integration test")
	}

	ctx := context.Background()
	project, err := loader.New().Load(ctx, "testdata/full-stack-project")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	reg := cloud.NewRegistry()
	for _, a := range []cloud.Adapter{aws.New(), azure.New(), gcp.New()} {
		if err := reg.Register(a); err != nil {
			t.Fatalf("register %s: %v", a.Name(), err)
		}
	}

	fakeRunner := tofu.NewFakeRunner()
	fakeRunner.PlanFileContents = []byte("FAKE-PLAN-BIN")

	workRoot := t.TempDir()
	p, err := provisioner.New(provisioner.Config{
		WorkRoot: workRoot,
		Adapters: reg,
		Runner:   fakeRunner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := p.Plan(ctx, provisioner.PlanInput{
		Project: project, Stack: "dev", OrgID: "test", DeploymentID: "dep-test",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.Targets) == 0 {
		t.Fatalf("Plan produced no targets")
	}

	env := append(os.Environ(),
		"AWS_ACCESS_KEY_ID=fake", "AWS_SECRET_ACCESS_KEY=fake",
		"ARM_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000000",
		"ARM_TENANT_ID=00000000-0000-0000-0000-000000000000",
		"ARM_CLIENT_ID=00000000-0000-0000-0000-000000000000",
		"ARM_CLIENT_SECRET=fake",
		"GOOGLE_PROJECT=fake-project",
		"GOOGLE_APPLICATION_CREDENTIALS=/dev/null",
	)

	for _, tp := range res.Targets {
		name := tp.Component + "/" + tp.Cloud + "/" + tp.Region
		t.Run(name, func(t *testing.T) {
			// Sanity-check JSON parseability first; gives a nicer error than tofu.
			body, err := os.ReadFile(filepath.Join(tp.WorkspaceDir, "main.tf.json"))
			if err != nil {
				t.Fatalf("read main.tf.json: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("main.tf.json not valid JSON: %v", err)
			}

			initCmd := exec.Command("tofu", "init", "-no-color", "-input=false")
			initCmd.Dir = tp.WorkspaceDir
			initCmd.Env = env
			if out, err := initCmd.CombinedOutput(); err != nil {
				t.Fatalf("tofu init failed: %v\n%s", err, out)
			}
			validateCmd := exec.Command("tofu", "validate", "-no-color")
			validateCmd.Dir = tp.WorkspaceDir
			validateCmd.Env = env
			if out, err := validateCmd.CombinedOutput(); err != nil {
				t.Errorf("tofu validate failed: %v\n%s", err, out)
			}
		})
	}
}
