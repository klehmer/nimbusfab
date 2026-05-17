package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type costEstimateRepo struct{ db *sql.DB }

// BulkInsert writes all items in a single transaction. Empty slice → no-op.
// Each row gets a fresh "cest-" UUID; callers don't supply IDs.
func (r *costEstimateRepo) BulkInsert(ctx context.Context, items []inventory.CostEstimate) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cost_estimates.BulkInsert begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO cost_estimates (id, org_id, run_id, primitive_id, currency, unit_price, units, unit_of_measure, subtotal, pricing_key_json)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
    `)
	if err != nil {
		return fmt.Errorf("cost_estimates.BulkInsert prepare: %w", err)
	}
	defer stmt.Close()
	for _, it := range items {
		id := "cest-" + uuid.NewString()
		if _, err := stmt.ExecContext(ctx, id, it.OrgID, it.RunID, it.PrimitiveID,
			it.Currency, it.UnitPrice, it.Units, it.UnitOfMeasure, it.Subtotal, jsonOrEmpty(it.PricingKeyJSON)); err != nil {
			return fmt.Errorf("cost_estimates.BulkInsert exec: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cost_estimates.BulkInsert commit: %w", err)
	}
	return nil
}

// ListByRun returns all estimates for one run, in insertion order. Caller
// scoped by org_id.
func (r *costEstimateRepo) ListByRun(ctx context.Context, orgID, runID string) ([]inventory.CostEstimate, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT run_id, org_id, primitive_id, currency, unit_price, units, unit_of_measure, subtotal,
               COALESCE(pricing_key_json::text, '')
        FROM cost_estimates WHERE org_id = $1 AND run_id = $2
        ORDER BY id
    `, orgID, runID)
	if err != nil {
		return nil, fmt.Errorf("cost_estimates.ListByRun: %w", err)
	}
	defer rows.Close()
	return scanCostEstimates(rows)
}

// ListByDeployment returns every estimate attached to any run of any target
// of the given deployment. JOINs runs → deployment_targets to filter.
func (r *costEstimateRepo) ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]inventory.CostEstimate, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT ce.run_id, ce.org_id, ce.primitive_id, ce.currency,
               ce.unit_price, ce.units, ce.unit_of_measure, ce.subtotal,
               COALESCE(ce.pricing_key_json::text, '')
        FROM cost_estimates ce
        JOIN runs r ON r.id = ce.run_id
        JOIN deployment_targets dt ON dt.id = r.deployment_target_id
        WHERE ce.org_id = $1 AND dt.deployment_id = $2
        ORDER BY ce.id
    `, orgID, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("cost_estimates.ListByDeployment: %w", err)
	}
	defer rows.Close()
	return scanCostEstimates(rows)
}

// scanCostEstimates drains rows; shared between ListByRun and ListByDeployment.
func scanCostEstimates(rows *sql.Rows) ([]inventory.CostEstimate, error) {
	var out []inventory.CostEstimate
	for rows.Next() {
		var e inventory.CostEstimate
		var pk string
		if err := rows.Scan(&e.RunID, &e.OrgID, &e.PrimitiveID, &e.Currency,
			&e.UnitPrice, &e.Units, &e.UnitOfMeasure, &e.Subtotal, &pk); err != nil {
			return nil, err
		}
		e.PricingKeyJSON = []byte(pk)
		out = append(out, e)
	}
	return out, rows.Err()
}
