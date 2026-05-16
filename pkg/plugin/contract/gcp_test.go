package contract_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/plugin/contract"
)

func TestGCPAdapter_ProvisionerContract(t *testing.T) {
	sample := ir.DeploymentTarget{
		Cloud:  "gcp",
		Region: "us-central1",
		Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web", "__type": "network"},
	}
	contract.RunProvisionerScenarios(t, gcp.New(), sample)
}
