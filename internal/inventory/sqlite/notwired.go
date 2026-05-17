package sqlite

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// notWired stubs — Phase 1 wires only the subset of repos that
// Plan/Apply/Destroy/Drift need; the others land in their owning phases.

type errUsers struct{}

func (errUsers) Get(ctx context.Context, orgID, id string) (*inventory.User, error) {
	return nil, inventory.ErrNotImplementedYet
}
func (errUsers) GetByEmail(ctx context.Context, orgID, email string) (*inventory.User, error) {
	return nil, inventory.ErrNotImplementedYet
}
func (errUsers) Create(ctx context.Context, u inventory.User) error {
	return inventory.ErrNotImplementedYet
}

type errCompositions struct{}

func (errCompositions) ListByProject(ctx context.Context, orgID, projectID string) ([]inventory.CompositionRecord, error) {
	return nil, inventory.ErrNotImplementedYet
}
func (errCompositions) Upsert(ctx context.Context, c inventory.CompositionRecord) error {
	return inventory.ErrNotImplementedYet
}

type errRunLogs struct{}

func (errRunLogs) Append(ctx context.Context, lines []inventory.RunLogLine) error {
	return inventory.ErrNotImplementedYet
}
func (errRunLogs) Read(ctx context.Context, orgID, runID string, sinceSeq int64) ([]inventory.RunLogLine, error) {
	return nil, inventory.ErrNotImplementedYet
}

type errCostAct struct{}

func (errCostAct) Upsert(ctx context.Context, rows []inventory.CostActual) error {
	return inventory.ErrNotImplementedYet
}
func (errCostAct) Query(ctx context.Context, q inventory.CostActualQuery) ([]inventory.CostActual, error) {
	return nil, inventory.ErrNotImplementedYet
}

type errSecrets struct{}

func (errSecrets) Get(ctx context.Context, orgID, name string) (*inventory.SecretsRef, error) {
	return nil, inventory.ErrNotImplementedYet
}
func (errSecrets) List(ctx context.Context, orgID string) ([]inventory.SecretsRef, error) {
	return nil, inventory.ErrNotImplementedYet
}
func (errSecrets) Upsert(ctx context.Context, r inventory.SecretsRef) error {
	return inventory.ErrNotImplementedYet
}
func (errSecrets) Delete(ctx context.Context, orgID, name string) error {
	return inventory.ErrNotImplementedYet
}
