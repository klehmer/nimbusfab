-- Phase 2 drift scheduler: per-deployment poll interval.
-- Postgres version.

ALTER TABLE deployments ADD COLUMN IF NOT EXISTS drift_interval_seconds INTEGER NOT NULL DEFAULT 0;
