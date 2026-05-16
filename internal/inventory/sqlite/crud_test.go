package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestProjectStackComponent_RoundTrip(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	if err := r.Orgs().Create(ctx, inventory.Org{ID: "org-1", Name: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "org-1", Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "org-1", ProjectID: "p-1", Name: "dev", StateBackendKind: "local", StateBackendCfg: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := r.Components().Upsert(ctx, inventory.Component{ID: "c-1", OrgID: "org-1", ProjectID: "p-1", StackID: "s-1", Name: "web", Type: "network", IRJSON: []byte(`{"name":"web"}`)}); err != nil {
		t.Fatal(err)
	}

	p, _ := r.Projects().Get(ctx, "org-1", "p-1")
	if p == nil || p.Name != "demo" {
		t.Fatalf("project: %+v", p)
	}
	s, _ := r.Stacks().GetByName(ctx, "org-1", "p-1", "dev")
	if s == nil || s.StateBackendKind != "local" {
		t.Fatalf("stack: %+v", s)
	}
	cs, _ := r.Components().ListByStack(ctx, "org-1", "p-1", "s-1")
	if len(cs) != 1 || cs[0].Name != "web" {
		t.Fatalf("components: %+v", cs)
	}
	if err := r.Components().Upsert(ctx, inventory.Component{ID: "c-1", OrgID: "org-1", ProjectID: "p-1", StackID: "s-1", Name: "web", Type: "network", IRJSON: []byte(`{"name":"web","updated":true}`)}); err != nil {
		t.Fatal(err)
	}
	cs2, _ := r.Components().ListByStack(ctx, "org-1", "p-1", "s-1")
	if string(cs2[0].IRJSON) != `{"name":"web","updated":true}` {
		t.Errorf("upsert update: %s", cs2[0].IRJSON)
	}
}

func TestOrgScoping_IsolatedReads(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "org-A", Name: "a"})
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "org-B", Name: "b"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "org-A", Name: "shared-name"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p2", OrgID: "org-B", Name: "shared-name"})

	listA, _ := r.Projects().List(ctx, "org-A")
	if len(listA) != 1 || listA[0].ID != "p" {
		t.Errorf("org A leak: %+v", listA)
	}
	listB, _ := r.Projects().List(ctx, "org-B")
	if len(listB) != 1 || listB[0].ID != "p2" {
		t.Errorf("org B leak: %+v", listB)
	}
}

func TestDeploymentRunLifecycle(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "org-1", Name: "local"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p-1", OrgID: "org-1", Name: "demo"})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s-1", OrgID: "org-1", ProjectID: "p-1", Name: "dev", StateBackendKind: "local"})

	dep := inventory.Deployment{
		ID: "d-1", OrgID: "org-1", ProjectID: "p-1", StackID: "s-1",
		Status: "planned", PartialFailurePolicy: "leave", StartedAt: time.Now().UTC(),
	}
	if err := r.Deployments().Create(ctx, dep); err != nil {
		t.Fatalf("dep create: %v", err)
	}

	tgt := inventory.DeploymentTarget{
		ID: "t-1", OrgID: "org-1", DeploymentID: "d-1",
		ComponentName: "web", Cloud: "aws", Region: "us-east-1", CredentialRef: "aws-dev",
		WorkspacePath: "/tmp/ws", PlanFile: "/tmp/plan.bin",
		StateBackend:  []byte(`{"kind":"local"}`),
		Status:        "planned", StartedAt: time.Now().UTC(),
	}
	if err := r.DeploymentTargets().Create(ctx, tgt); err != nil {
		t.Fatalf("tgt create: %v", err)
	}

	run := inventory.Run{
		ID: "r-1", OrgID: "org-1", DeploymentTargetID: "t-1",
		Kind: "apply", Status: "running", StartedAt: time.Now().UTC(),
	}
	if err := r.Runs().Create(ctx, run); err != nil {
		t.Fatalf("run create: %v", err)
	}

	finished := time.Now().UTC().Add(time.Minute)
	if err := r.Runs().UpdateStatus(ctx, "org-1", "r-1", "succeeded", 0, &finished); err != nil {
		t.Fatal(err)
	}
	if err := r.DeploymentTargets().UpdateStatus(ctx, "org-1", "t-1", "succeeded", &finished); err != nil {
		t.Fatal(err)
	}
	if err := r.Deployments().UpdateStatus(ctx, "org-1", "d-1", "succeeded", &finished); err != nil {
		t.Fatal(err)
	}

	d, _ := r.Deployments().Get(ctx, "org-1", "d-1")
	if d == nil || d.Status != "succeeded" || d.FinishedAt == nil {
		t.Errorf("deployment terminal: %+v", d)
	}
	runs, _ := r.Runs().ListByDeploymentTarget(ctx, "org-1", "t-1")
	if len(runs) != 1 || runs[0].Status != "succeeded" {
		t.Errorf("runs: %+v", runs)
	}
	targets, _ := r.DeploymentTargets().ListByDeployment(ctx, "org-1", "d-1")
	if len(targets) != 1 || targets[0].PlanFile != "/tmp/plan.bin" {
		t.Errorf("targets: %+v", targets)
	}
}

func TestDrift_UpsertReplaces(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "o", Name: "x"})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "o", ProjectID: "p", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "o", ProjectID: "p", StackID: "s", Status: "planned", StartedAt: time.Now()})
	_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t", OrgID: "o", DeploymentID: "d", ComponentName: "web", Cloud: "aws", Region: "us-east-1", CredentialRef: "r", Status: "planned", StartedAt: time.Now()})

	_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
		DeploymentTargetID: "t", OrgID: "o", DetectedAt: time.Now().UTC(), HasDrift: true, SummaryJSON: []byte(`{"v":1}`),
	})
	_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
		DeploymentTargetID: "t", OrgID: "o", DetectedAt: time.Now().UTC(), HasDrift: false, SummaryJSON: []byte(`{"v":2}`),
	})
	d, _ := r.DriftStatus().Get(ctx, "o", "t")
	if d == nil || d.HasDrift {
		t.Errorf("upsert should replace: %+v", d)
	}
	if string(d.SummaryJSON) != `{"v":2}` {
		t.Errorf("summary not replaced: %s", d.SummaryJSON)
	}
}
