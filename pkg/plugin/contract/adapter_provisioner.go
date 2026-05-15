package contract

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// RunProvisionerScenarios is the v1 add-on suite for provisioner-era contract
// methods. Adapters that participate in `nimbusfab plan` MUST pass this suite.
// It does NOT depend on PricingKey or BillingQuery being wired (those land in
// later phases), so Phase-1 stub adapters can run this independently of
// RunAdapterSuite.
func RunProvisionerScenarios(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
	t.Helper()
	t.Run("name_is_stable", func(t *testing.T) { NameIsStable(t, a) })
	t.Run("supports_at_least_one_apiver", func(t *testing.T) { SupportsAtLeastOneAPIVersion(t, a) })
	t.Run("supports_at_least_one_type", func(t *testing.T) { SupportsAtLeastOneComponentType(t, a) })
	t.Run("tier_one_schema_is_valid_json", func(t *testing.T) { TierOneSchemaIsValidJSON(t, a) })
	t.Run("provider_block_no_plaintext", func(t *testing.T) { ProviderBlockNoPlaintextSecrets(t, a, sample) })
	t.Run("default_state_backend_kind_set", func(t *testing.T) { DefaultStateBackendKindSet(t, a, sample) })
	t.Run("emit_is_pure", func(t *testing.T) { EmitIsPure(t, a, sample) })
}

// NameIsStable asserts Adapter.Name() returns a non-empty, stable value.
func NameIsStable(t *testing.T, a cloud.Adapter) {
	t.Helper()
	if a.Name() == "" {
		t.Error("Name() returned empty string")
	}
	if a.Name() != a.Name() {
		t.Error("Name() not stable across calls")
	}
}

func SupportsAtLeastOneAPIVersion(t *testing.T, a cloud.Adapter) {
	t.Helper()
	if len(a.SupportedAPIVersions()) == 0 {
		t.Error("SupportedAPIVersions() returned empty slice")
	}
}

func SupportsAtLeastOneComponentType(t *testing.T, a cloud.Adapter) {
	t.Helper()
	if len(a.SupportedComponentTypes()) == 0 {
		t.Error("SupportedComponentTypes() returned empty slice")
	}
}

func TierOneSchemaIsValidJSON(t *testing.T, a cloud.Adapter) {
	t.Helper()
	var v any
	if err := json.Unmarshal(a.TierOneSchema(), &v); err != nil {
		t.Errorf("TierOneSchema(): not valid JSON: %v", err)
	}
}

func ProviderBlockNoPlaintextSecrets(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
	t.Helper()
	pb, err := a.ProviderBlock(context.Background(), sample, cloud.Credentials{Ref: "test"})
	if err != nil {
		t.Fatalf("ProviderBlock: %v", err)
	}
	raw, _ := json.Marshal(pb)
	lower := strings.ToLower(string(raw))
	forbidden := []string{"access_key", "secret_key", "password", "private_key", "client_secret"}
	for _, f := range forbidden {
		if strings.Contains(lower, f) {
			t.Errorf("ProviderBlock contains forbidden key %q: %s", f, raw)
		}
	}
}

func DefaultStateBackendKindSet(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
	t.Helper()
	sb, err := a.DefaultStateBackend(context.Background(), sample)
	if err != nil {
		t.Fatalf("DefaultStateBackend: %v", err)
	}
	if sb.Kind == "" {
		t.Error("DefaultStateBackend.Kind = \"\"")
	}
}

func EmitIsPure(t *testing.T, a cloud.Adapter, sample ir.DeploymentTarget) {
	t.Helper()
	a1, err := a.Emit(context.Background(), sample, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit (first call): %v", err)
	}
	a2, err := a.Emit(context.Background(), sample, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit (second call): %v", err)
	}
	j1, _ := json.Marshal(a1)
	j2, _ := json.Marshal(a2)
	if string(j1) != string(j2) {
		t.Errorf("Emit not pure:\n call1: %s\n call2: %s", j1, j2)
	}
}
