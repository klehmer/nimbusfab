// Package contract provides reusable tests every cloud.Adapter must pass.
// In-tree adapters call RunAdapterSuite from their own *_test.go files; the
// v2 gRPC plugin host will run the same suite against out-of-process plugins.
// Failures here are public-API regressions — fix the adapter, not the test.
package contract

import (
	"context"
	"testing"
	"time"

	"github.com/kratus8990/nimbusfab/pkg/cloud"
	"github.com/kratus8990/nimbusfab/pkg/ir"
)

// SuiteOptions parameterizes which checks RunAdapterSuite executes.
type SuiteOptions struct {
	// Adapter is the implementation under test.
	Adapter cloud.Adapter

	// SampleTargets are realistic DeploymentTarget fixtures the adapter
	// claims to support. Each is exercised through Emit and PricingKey.
	SampleTargets []ir.DeploymentTarget

	// SkipBilling tells the suite not to call BillingQuery / FetchBilling.
	// Useful when integration credentials are not available.
	SkipBilling bool

	// BillingCreds is used when SkipBilling is false.
	BillingCreds cloud.Credentials
}

// RunAdapterSuite is the entry point in-tree adapter tests call. It is a
// table-driven suite of subtests using t.Run; each subtest is independent and
// can be re-run in isolation.
func RunAdapterSuite(t *testing.T, opts SuiteOptions) {
	t.Helper()

	if opts.Adapter == nil {
		t.Fatal("contract.RunAdapterSuite: SuiteOptions.Adapter is required")
	}

	t.Run("Name_stable", func(t *testing.T) {
		if opts.Adapter.Name() == "" {
			t.Fatal("Adapter.Name() returned empty string")
		}
	})

	t.Run("SupportedAPIVersions_nonempty", func(t *testing.T) {
		if len(opts.Adapter.SupportedAPIVersions()) == 0 {
			t.Fatal("Adapter.SupportedAPIVersions() returned empty slice")
		}
	})

	t.Run("Emit_pure_and_deterministic", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for i, target := range opts.SampleTargets {
			t.Run(target.Cloud+"_"+target.Region, func(t *testing.T) {
				first, err := opts.Adapter.Emit(ctx, target, nil)
				if err != nil {
					t.Fatalf("sample %d: first Emit() error: %v", i, err)
				}
				second, err := opts.Adapter.Emit(ctx, target, nil)
				if err != nil {
					t.Fatalf("sample %d: second Emit() error: %v", i, err)
				}
				if len(first) != len(second) {
					t.Fatalf("sample %d: Emit() is non-deterministic: got %d then %d primitives",
						i, len(first), len(second))
				}
				for j := range first {
					if first[j].ID != second[j].ID || first[j].TofuType != second[j].TofuType {
						t.Fatalf("sample %d primitive %d: Emit() drift: %+v vs %+v",
							i, j, first[j], second[j])
					}
				}
			})
		}
	})

	t.Run("PricingKey_for_each_primitive", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for _, target := range opts.SampleTargets {
			primitives, err := opts.Adapter.Emit(ctx, target, nil)
			if err != nil {
				t.Fatalf("Emit for pricing key test failed: %v", err)
			}
			for _, p := range primitives {
				key, err := opts.Adapter.PricingKey(ctx, p)
				if err != nil {
					t.Fatalf("PricingKey(%s) error: %v", p.ID, err)
				}
				if len(key) == 0 {
					t.Fatalf("PricingKey(%s) returned empty key", p.ID)
				}
			}
		}
	})

	if !opts.SkipBilling {
		t.Run("BillingQuery_returns_params", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			params, err := opts.Adapter.BillingQuery(ctx, opts.BillingCreds, time.Now().Add(-24*time.Hour), time.Now())
			if err != nil {
				t.Fatalf("BillingQuery error: %v", err)
			}
			if params == nil {
				t.Fatal("BillingQuery returned nil params")
			}
		})
	}

	t.Run("DefaultStateBackend_non_empty_kind", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if len(opts.SampleTargets) == 0 {
			t.Skip("no sample targets")
		}
		sb, err := opts.Adapter.DefaultStateBackend(ctx, opts.SampleTargets[0])
		if err != nil {
			t.Fatalf("DefaultStateBackend error: %v", err)
		}
		if sb.Kind == "" {
			t.Fatal("DefaultStateBackend returned empty kind")
		}
	})
}
