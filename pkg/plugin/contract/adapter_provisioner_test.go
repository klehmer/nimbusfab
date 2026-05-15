package contract_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/plugin/contract"
)

func TestAWSAdapter_ProvisionerContract(t *testing.T) {
	sample := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web"},
	}
	contract.RunProvisionerScenarios(t, aws.New(), sample)
}

func TestFakeAdapter_ProvisionerContract(t *testing.T) {
	sample := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{},
	}
	contract.RunProvisionerScenarios(t, cloud.NewFakeAdapter("aws"), sample)
}
