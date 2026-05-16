# AWS Pricing Snapshot

Manually curated on-demand prices for the AWS resource types the AWS adapter
(`internal/cloud/aws`) emits in Phase 3. All prices in USD as of the
snapshot's `fetchedAt`.

## Format

See `aws.json`. Top-level shape:

```json
{
  "$schema": "https://nimbusfab.dev/schema/pricing/snapshot/v1alpha1.json",
  "cloud": "aws",
  "currency": "USD",
  "fetchedAt": "ISO-8601 timestamp",
  "source": "manual-curation | live-fetch | ...",
  "entries": [ ... ]
}
```

Each entry:

```json
{
  "key": { /* same shape as Adapter.PricingKey() */ },
  "unitPrice": 0.0208,
  "unitOfMeasure": "Hrs"
}
```

Common `unitOfMeasure` values:
- `Hrs` — EC2 instances, RDS instances
- `GB-Mo` — S3 standard storage

## Coverage

Phase 1 covers what the AWS adapter actually emits:

- **EC2**: t3.{small,medium,large,xlarge} + m6i.{large,xlarge} × {us-east-1, us-west-2, eu-west-1} Linux on-demand.
- **RDS**: db.t3.{small,medium} + db.m6i.{large,xlarge} × {postgres, mysql, mariadb} × {Single-AZ, Multi-AZ} (most combinations covered for us-east-1; other regions add as needed).
- **S3 Standard**: us-east-1, us-west-2, eu-west-1.

When a key is missing, the estimator emits a warning ("missing pricing for X")
but continues — partial estimates are better than no estimate.

## Refreshing

Phase 1 is manual. To refresh:

1. Pull current AWS On-Demand prices per region from
   https://aws.amazon.com/ec2/pricing/on-demand/, RDS pricing, S3 pricing.
2. Update `unitPrice` for each entry.
3. Update top-level `fetchedAt` to today (UTC).
4. Optionally update `source` to describe the refresh method.
5. Run `go test ./pkg/cost/...` and confirm all bundled-snapshot tests pass.

Cost Phase 2 will add live AWS Pricing API integration plus a
`tools/pricing/verify/` program to diff snapshot vs. live and flag stale rows.
