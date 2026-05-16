package azure

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func emitStorageImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "storage"
	}
	name := tofuIdent(component)
	rgName := resourceGroupName(component, target.Region)

	accountName, _ := target.Spec["name"].(string)
	if accountName == "" {
		accountName = deriveStorageAccountName(component, target.Region)
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
				"blob_properties": []any{map[string]any{
					"versioning_enabled": versioning,
				}},
			},
		},
		{
			ID:       fmt.Sprintf("%s.azure-%s.container", component, target.Region),
			Cloud:    "azure",
			TofuType: "azurerm_storage_container",
			TofuName: name,
			Attributes: map[string]any{
				"name":                  "default",
				"storage_account_name":  "${azurerm_storage_account." + name + ".name}",
				"container_access_type": "private",
			},
		},
	}, nil
}

// deriveStorageAccountName produces an Azure-legal name (3-24 chars, lowercase
// letters + digits only) from component + region. Deterministic.
func deriveStorageAccountName(component, region string) string {
	clean := strings.Builder{}
	for _, c := range strings.ToLower(component) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			clean.WriteRune(c)
		}
	}
	base := clean.String()
	if len(base) > 18 {
		base = base[:18]
	}
	sum := sha256.Sum256([]byte(component + ":" + region))
	suffix := hex.EncodeToString(sum[:])[:6]
	return base + suffix
}
