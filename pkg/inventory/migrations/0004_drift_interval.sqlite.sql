-- Phase 2 drift scheduler: per-deployment poll interval.
-- SQLite version.

ALTER TABLE deployments ADD COLUMN drift_interval_seconds INTEGER NOT NULL DEFAULT 0;
