package azure

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func emitStorageImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "storage"
	}
	deploymentID, _ := target.Spec["__deployment_id"].(string)
	name := tofuIdent(component)
	rgName := resourceGroupName(component, target.Region)

	accountName, _ := target.Spec["name"].(string)
	if accountName == "" {
		accountName = azureStorageAccountName(component, deploymentID)
	}
	if len(accountName) < 3 || len(accountName) > 24 {
		return nil, fmt.Errorf("azure.emitStorage: storage account name %q must be 3-24 chars (got %d)", accountName, len(accountName))
	}

	versioning := boolFromSpec(target.Spec, "versioning", true)
	publicAccess, _ := target.Spec["publicAccess"].(string)
	publicEnabled := publicAccess == "allowed"

	return []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.azure-%s.rg", component, target.Region),
			Cloud:    "azure",
			TofuType: "azurerm_resource_group",
			TofuName: name,
			Attributes: map[string]any{
				"name":     rgName,
				"location": target.Region,
			},
		},
		{
			ID:       fmt.Sprintf("%s.azure-%s.account", component, target.Region),
			Cloud:    "azure",
			TofuType: "azurerm_storage_account",
			TofuName: name,
			Attributes: map[string]any{
				"name":                            accountName,
				"resource_group_name":             "${azurerm_resource_group." + name + ".name}",
				"location":                        target.Region,
				"account_tier":                    "Standard",
				"account_replication_type":        "LRS",
				"account_kind":                    "StorageV2",
				"public_network_access_enabled":   publicEnabled,
				"allow_nested_items_to_be_public": publicEnabled,
				"min_tls_version":                 "TLS1_2",
				"blob_properties": []any{map[string]any{
					"versioning_enabled": versioning,
				}},
			},
		},
		{
			ID:           fmt.Sprintf("%s.azure-%s.container", component, target.Region),
			Cloud:        "azure",
			TofuType:     "azurerm_storage_container",
			TofuName:     name,
			TagAttribute: ir.TagAttributeSkip, // azurerm_storage_container does not support tags
			Attributes: map[string]any{
				"name":                  "default",
				"storage_account_name":  "${azurerm_storage_account." + name + ".name}",
				"container_access_type": "private",
			},
		},
	}, nil
}

