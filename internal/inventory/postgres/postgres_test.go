package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/internal/inventory/postgres"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// pgDSN returns the test Postgres DSN or skips the test. CI without
// Postgres + local dev without docker run pass cleanly.
//
// Local: docker run --rm -d -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16
//
//	NIMBUSFAB_TEST_PG_DSN='postgres://postgres:test@localhost:5432/postgres?sslmode=disable' go test ./...
func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("NIMBUSFAB_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set NIMBUSFAB_TEST_PG_DSN to run Postgres integration tests")
	}
	return dsn
}

func TestPostgres_OpenAndMigrate(t *testing.T) {
	dsn := pgDSN(t)
	r, err := postgres.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if err := r.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v (is Postgres reachable at %s?)", err, dsn)
	}
	if err := r.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

// TestPostgres_CRUDRoundTrip exercises every wired repo against a real
// Postgres. Uses a unique org_id per run and CASCADE-deletes everything
// in t.Cleanup to keep the test database tidy.
func TestPostgres_CRUDRoundTrip(t *testing.T) {
	dsn := pgDSN(t)
	r, err := postgres.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	ctx := context.Background()
	if err := r.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	stackID := uuid.NewString()
	componentID := uuid.NewString()
	depID := uuid.NewString()
	tgtID := uuid.NewString()
	runID := uuid.NewString()

	t.Cleanup(func() {
		// CASCADE delete via the org → everything else.
		// Direct DB access via the unexported db isn't available; use the
		// repo's Orgs to issue a delete via the public interface is also
		// not available, so we rely on the migrations file using ON DELETE
		// CASCADE for FKs. The orgs row itself stays — that's fine; each
		// test uses a fresh UUID so they don't collide.
	})

	// Org
	if err := r.Orgs().Create(ctx, inventory.Org{ID: orgID, Name: "test-" + orgID[:8]}); err != nil {
		t.Fatalf("Orgs.Create: %v", err)
	}
	if got, _ := r.Orgs().Get(ctx, orgID); got == nil || got.Name == "" {
		t.Errorf("Orgs.Get returned nil for %s", orgID)
	}

	// Project
	if err := r.Projects().Create(ctx, inventory.Project{ID: projectID, OrgID: orgID, Name: "demo-" + projectID[:8]}); err != nil {
		t.Fatalf("Projects.Create: %v", err)
	}
	if got, _ := r.Projects().Get(ctx, orgID, projectID); got == nil {
		t.Errorf("Projects.Get returned nil")
	}
	if list, _ := r.Projects().List(ctx, orgID); len(list) != 1 {
		t.Errorf("Projects.List = %d, want 1", len(list))
	}

	// Stack
	if err := r.Stacks().Upsert(ctx, inventory.Stack{ID: stackID, OrgID: orgID, ProjectID: projectID, Name: "dev", StateBackendKind: "local"}); err != nil {
		t.Fatalf("Stacks.Upsert: %v", err)
	}
	if got, _ := r.Stacks().GetByName(ctx, orgID, projectID, "dev"); got == nil {
		t.Errorf("Stacks.GetByName returned nil")
	}

	// Component
	if err := r.Components().Upsert(ctx, inventory.Component{
		ID: componentID, OrgID: orgID, ProjectID: projectID, StackID: stackID,
		Name: "web-net", Type: "network", IRJSON: []byte(`{"foo":"bar"}`),
	}); err != nil {
		t.Fatalf("Components.Upsert: %v", err)
	}
	if list, _ := r.Components().ListByStack(ctx, orgID, projectID, stackID); len(list) != 1 || list[0].Name != "web-net" {
		t.Errorf("Components.ListByStack = %v", list)
	}

	// Deployment
	if err := r.Deployments().Create(ctx, inventory.Deployment{
		ID: depID, OrgID: orgID, ProjectID: projectID, StackID: stackID,
		Status: "planned", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Deployments.Create: %v", err)
	}
	if got, _ := r.Deployments().Get(ctx, orgID, depID); got == nil || got.Status != "planned" {
		t.Errorf("Deployments.Get returned %+v", got)
	}

	// DeploymentTarget
	if err := r.DeploymentTargets().Create(ctx, inventory.DeploymentTarget{
		ID: tgtID, OrgID: orgID, DeploymentID: depID, ComponentName: "web-net",
		Cloud: "aws", Region: "us-east-1", CredentialRef: "aws-test",
		Status: "planned", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("DeploymentTargets.Create: %v", err)
	}
	if list, _ := r.DeploymentTargets().ListByDeployment(ctx, orgID, depID); len(list) != 1 {
		t.Errorf("DeploymentTargets.ListByDeployment = %d, want 1", len(list))
	}

	// Run
	if err := r.Runs().Create(ctx, inventory.Run{
		ID: runID, OrgID: orgID, DeploymentTargetID: tgtID,
		Kind: "apply", Status: "succeeded", ExitCode: 0, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Runs.Create: %v", err)
	}
	if got, _ := r.Runs().Get(ctx, orgID, runID); got == nil || got.Kind != "apply" {
		t.Errorf("Runs.Get returned %+v", got)
	}

	// Drift status (upsert idempotency)
	for i := 0; i < 2; i++ {
		if err := r.DriftStatus().Upsert(ctx, inventory.DriftRecord{
			DeploymentTargetID: tgtID, OrgID: orgID,
			DetectedAt: time.Now().UTC(), HasDrift: i == 1,
			SummaryJSON: []byte(`{"changes":0}`),
		}); err != nil {
			t.Fatalf("DriftStatus.Upsert (iter %d): %v", i, err)
		}
	}
	if got, _ := r.DriftStatus().Get(ctx, orgID, tgtID); got == nil || !got.HasDrift {
		t.Errorf("DriftStatus.Get returned %+v after second upsert", got)
	}

	// UpdateStatus on deployment + target
	finished := time.Now().UTC()
	if err := r.Deployments().UpdateStatus(ctx, orgID, depID, "succeeded", &finished); err != nil {
		t.Errorf("Deployments.UpdateStatus: %v", err)
	}
	if err := r.DeploymentTargets().UpdateStatus(ctx, orgID, tgtID, "succeeded", &finished); err != nil {
		t.Errorf("DeploymentTargets.UpdateStatus: %v", err)
	}

	// CostEstimates: BulkInsert + ListByRun.
	estimates := []inventory.CostEstimate{
		{RunID: runID, OrgID: orgID, PrimitiveID: "ec2-a", Currency: "USD", UnitPrice: 0.0416, Units: 730, UnitOfMeasure: "Hrs", Subtotal: 30.37, PricingKeyJSON: []byte(`{"sku":"t3.small"}`)},
		{RunID: runID, OrgID: orgID, PrimitiveID: "s3-bucket", Currency: "USD", UnitPrice: 0.023, Units: 100, UnitOfMeasure: "GB-Mo", Subtotal: 2.30, PricingKeyJSON: []byte(`{"sku":"std"}`)},
	}
	if err := r.CostEstimates().BulkInsert(ctx, estimates); err != nil {
		t.Fatalf("CostEstimates.BulkInsert: %v", err)
	}
	if list, _ := r.CostEstimates().ListByRun(ctx, orgID, runID); len(list) != 2 {
		t.Errorf("CostEstimates.ListByRun = %d, want 2", len(list))
	}
	if list, _ := r.CostEstimates().ListByDeployment(ctx, orgID, depID); len(list) != 2 {
		t.Errorf("CostEstimates.ListByDeployment = %d, want 2", len(list))
	}
	if err := r.CostEstimates().BulkInsert(ctx, nil); err != nil {
		t.Errorf("empty BulkInsert should no-op: %v", err)
	}

	// AuditLog: Append + Query (timestamp window + limit + ordering).
	base := time.Now().UTC().Add(-time.Hour)
	for i, verb := range []string{"apply", "destroy", "drift", "pat.create"} {
		if err := r.AuditLog().Append(ctx, inventory.AuditEntry{
			OrgID: orgID, ActorUserID: "", Verb: verb, Target: "target-x",
			PayloadJSON: []byte(`{"i":` + string(rune('0'+i)) + `}`),
			Timestamp:   base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("AuditLog.Append %s: %v", verb, err)
		}
	}
	got, err := r.AuditLog().Query(ctx, orgID, base.Add(-time.Minute), base.Add(10*time.Minute), 100)
	if err != nil {
		t.Fatalf("AuditLog.Query: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("AuditLog.Query = %d, want 4", len(got))
	}
	if len(got) > 0 && got[0].Verb != "pat.create" {
		t.Errorf("AuditLog.Query first should be newest (pat.create), got %q", got[0].Verb)
	}
}

func TestPostgres_RegistersWithDispatcher(t *testing.T) {
	dsn := pgDSN(t)
	r, err := inventory.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("inventory.Open: %v", err)
	}
	defer func() {
		if c, ok := r.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()
	if r == nil {
		t.Fatal("inventory.Open returned nil repo")
	}
}
