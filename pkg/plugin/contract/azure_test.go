package contract_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/plugin/contract"
)

func TestAzureAdapter_ProvisionerContract(t *testing.T) {
	sample := ir.DeploymentTarget{
		Cloud:  "azure",
		Region: "eastus",
		Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web", "__type": "network"},
	}
	contract.RunProvisionerScenarios(t, azure.New(), sample)
}
