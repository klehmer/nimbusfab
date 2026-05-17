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
		StateBackend: []byte(`{"kind":"local"}`),
		Status:       "planned", StartedAt: time.Now().UTC(),
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

func TestSQLite_CostEstimates_RoundTrip(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	// Seed the FK chain runs requires.
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "o", Name: "x"})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "o", ProjectID: "p", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "o", ProjectID: "p", StackID: "s", Status: "planned", StartedAt: time.Now()})
	_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{ID: "t", OrgID: "o", DeploymentID: "d", ComponentName: "web", Cloud: "aws", Region: "us-east-1", CredentialRef: "r", Status: "planned", StartedAt: time.Now()})
	_ = r.Runs().Create(ctx, inventory.Run{ID: "run-1", OrgID: "o", DeploymentTargetID: "t", Kind: "plan", Status: "succeeded", StartedAt: time.Now()})

	items := []inventory.CostEstimate{
		{RunID: "run-1", OrgID: "o", PrimitiveID: "ec2-a", Currency: "USD", UnitPrice: 0.0416, Units: 730, UnitOfMeasure: "Hrs", Subtotal: 30.37, PricingKeyJSON: []byte(`{"sku":"t3.small"}`)},
		{RunID: "run-1", OrgID: "o", PrimitiveID: "ec2-b", Currency: "USD", UnitPrice: 0.0832, Units: 730, UnitOfMeasure: "Hrs", Subtotal: 60.74, PricingKeyJSON: []byte(`{"sku":"t3.medium"}`)},
		{RunID: "run-1", OrgID: "o", PrimitiveID: "s3-bucket", Currency: "USD", UnitPrice: 0.023, Units: 100, UnitOfMeasure: "GB-Mo", Subtotal: 2.30, PricingKeyJSON: []byte(`{"sku":"std"}`)},
	}
	if err := r.CostEstimates().BulkInsert(ctx, items); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	got, err := r.CostEstimates().ListByRun(ctx, "o", "run-1")
	if err != nil {
		t.Fatalf("ListByRun: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Spot-check one row keeps all fields.
	for _, e := range got {
		if e.PrimitiveID == "ec2-a" {
			if e.Subtotal != 30.37 || e.UnitOfMeasure != "Hrs" || string(e.PricingKeyJSON) != `{"sku":"t3.small"}` {
				t.Errorf("ec2-a fields mangled: %+v", e)
			}
		}
	}

	// Empty input is a no-op (defensive).
	if err := r.CostEstimates().BulkInsert(ctx, nil); err != nil {
		t.Errorf("empty BulkInsert: %v", err)
	}

	// Wrong org isolation.
	if list, _ := r.CostEstimates().ListByRun(ctx, "other-org", "run-1"); len(list) != 0 {
		t.Errorf("wrong-org lookup returned %d rows", len(list))
	}
}

func TestSQLite_AuditLog_RoundTrip(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})

	base := time.Now().UTC().Truncate(time.Second)
	entries := []inventory.AuditEntry{
		{OrgID: "o", ActorUserID: "u1", Verb: "apply", Target: "deployment-1", PayloadJSON: []byte(`{"ok":true}`), Timestamp: base.Add(-3 * time.Hour)},
		{OrgID: "o", ActorUserID: "u2", Verb: "destroy", Target: "deployment-2", Timestamp: base.Add(-2 * time.Hour)},
		{OrgID: "o", Verb: "pat.create", Target: "pat-1", Timestamp: base.Add(-1 * time.Hour)},
		{OrgID: "o", ActorUserID: "u1", Verb: "drift", Target: "deployment-1", Timestamp: base},
	}
	for _, e := range entries {
		if err := r.AuditLog().Append(ctx, e); err != nil {
			t.Fatalf("Append %s: %v", e.Verb, err)
		}
	}

	got, err := r.AuditLog().Query(ctx, "o", base.Add(-4*time.Hour), base, 100)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("Query len = %d, want 4", len(got))
	}
	// DESC order: most recent first.
	if got[0].Verb != "drift" {
		t.Errorf("first entry should be most recent (drift), got %q", got[0].Verb)
	}
	if got[3].Verb != "apply" {
		t.Errorf("last entry should be oldest (apply), got %q", got[3].Verb)
	}

	// Limit caps results.
	if got, _ := r.AuditLog().Query(ctx, "o", base.Add(-4*time.Hour), base, 2); len(got) != 2 {
		t.Errorf("limit=2: got %d", len(got))
	}

	// Time-window narrows.
	narrow, _ := r.AuditLog().Query(ctx, "o", base.Add(-90*time.Minute), base.Add(-30*time.Minute), 100)
	if len(narrow) != 1 || narrow[0].Verb != "pat.create" {
		t.Errorf("window query: %v", narrow)
	}

	// Wrong-org isolation.
	if other, _ := r.AuditLog().Query(ctx, "other-org", base.Add(-4*time.Hour), base, 100); len(other) != 0 {
		t.Errorf("wrong-org query returned %d", len(other))
	}
}

func TestSQLite_AuditLog_DefaultsTimestampToNow(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})

	before := time.Now().UTC().Add(-time.Second)
	if err := r.AuditLog().Append(ctx, inventory.AuditEntry{OrgID: "o", Verb: "noop"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, _ := r.AuditLog().Query(ctx, "o", before, time.Now().UTC().Add(time.Second), 1)
	if len(got) != 1 {
		t.Fatalf("got %d entries", len(got))
	}
	if got[0].Timestamp.Before(before) {
		t.Errorf("timestamp not auto-assigned: %v", got[0].Timestamp)
	}
}

func TestSQLite_CostEstimates_ListByDeployment(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()

	// Set up two targets under one deployment, each with one plan run + estimates.
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "o", Name: "x"})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "o", ProjectID: "p", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "o", ProjectID: "p", StackID: "s", Status: "planned", StartedAt: time.Now()})

	for _, target := range []struct{ id, cloud string }{{"t-aws", "aws"}, {"t-azure", "azure"}} {
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
			ID: target.id, OrgID: "o", DeploymentID: "d",
			ComponentName: "web", Cloud: target.cloud, Region: "r", CredentialRef: "x",
			Status: "planned", StartedAt: time.Now(),
		})
		runID := "run-" + target.id
		_ = r.Runs().Create(ctx, inventory.Run{
			ID: runID, OrgID: "o", DeploymentTargetID: target.id,
			Kind: "plan", Status: "succeeded", StartedAt: time.Now(),
		})
		_ = r.CostEstimates().BulkInsert(ctx, []inventory.CostEstimate{
			{RunID: runID, OrgID: "o", PrimitiveID: target.cloud + "-vm", Currency: "USD",
				UnitPrice: 0.05, Units: 730, UnitOfMeasure: "Hrs", Subtotal: 36.5,
				PricingKeyJSON: []byte(`{"sku":"x"}`)},
		})
	}

	got, err := r.CostEstimates().ListByDeployment(ctx, "o", "d")
	if err != nil {
		t.Fatalf("ListByDeployment: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Both target subtotals should be present.
	sum := 0.0
	for _, e := range got {
		sum += e.Subtotal
	}
	if sum < 72.9 || sum > 73.1 {
		t.Errorf("sum = %.2f, want ~73.00", sum)
	}

	// Wrong org isolation.
	if other, _ := r.CostEstimates().ListByDeployment(ctx, "other", "d"); len(other) != 0 {
		t.Errorf("wrong-org returned %d rows", len(other))
	}
}

func TestSQLite_DriftStatus_ListByOrg(t *testing.T) {
	r := openMemory(t)
	ctx := context.Background()
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "o", Name: "o"})
	_ = r.Orgs().Create(ctx, inventory.Org{ID: "other", Name: "other"})
	_ = r.Projects().Create(ctx, inventory.Project{ID: "p", OrgID: "o", Name: "x"})
	_ = r.Stacks().Upsert(ctx, inventory.Stack{ID: "s", OrgID: "o", ProjectID: "p", Name: "dev"})
	_ = r.Deployments().Create(ctx, inventory.Deployment{ID: "d", OrgID: "o", ProjectID: "p", StackID: "s", Status: "planned", StartedAt: time.Now()})

	base := time.Now().UTC().Truncate(time.Second)
	for i, target := range []struct{ id string }{{"t1"}, {"t2"}, {"t3"}} {
		_ = r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
			ID: target.id, OrgID: "o", DeploymentID: "d",
			ComponentName: "web", Cloud: "aws", Region: "us-east-1", CredentialRef: "x",
			Status: "planned", StartedAt: time.Now(),
		})
		_ = r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
			DeploymentTargetID: target.id, OrgID: "o",
			DetectedAt: base.Add(time.Duration(i) * time.Minute),
			HasDrift:   i == 1,
			SummaryJSON: []byte(`{"i":` + string(rune('0'+i)) + `}`),
		})
	}

	got, err := r.DriftStatus().ListByOrg(ctx, "o")
	if err != nil {
		t.Fatalf("ListByOrg: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// DESC by detected_at: t3 (latest) first.
	if got[0].DeploymentTargetID != "t3" {
		t.Errorf("first record should be t3 (newest); got %q", got[0].DeploymentTargetID)
	}
	// Wrong-org isolation.
	if other, _ := r.DriftStatus().ListByOrg(ctx, "other"); len(other) != 0 {
		t.Errorf("wrong-org returned %d rows", len(other))
	}
}
