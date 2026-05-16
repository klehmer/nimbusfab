package azure

import (
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func pricingKeyImpl(p ir.ResourcePrimitive) (map[string]any, error) {
	region := regionFromID(p.ID)
	switch p.TofuType {
	case "azurerm_linux_virtual_machine":
		sku, _ := p.Attributes["size"].(string)
		return map[string]any{
			"service":       "VirtualMachines",
			"armSkuName":    sku,
			"armRegionName": region,
			"priceType":     "Consumption",
			"productName":   "Virtual Machines BS Series",
		}, nil
	case "azurerm_postgresql_flexible_server":
		sku, _ := p.Attributes["sku_name"].(string)
		tier := dbTierFromSKU(sku)
		return map[string]any{
			"service":       "AzureDatabaseforPostgreSQL",
			"skuName":       sku,
			"armRegionName": region,
			"tier":          tier,
			"priceType":     "Consumption",
		}, nil
	case "azurerm_mysql_flexible_server":
		sku, _ := p.Attributes["sku_name"].(string)
		tier := dbTierFromSKU(sku)
		return map[string]any{
			"service":       "AzureDatabaseforMySQL",
			"skuName":       sku,
			"armRegionName": region,
			"tier":          tier,
			"priceType":     "Consumption",
		}, nil
	case "azurerm_mariadb_server":
		// Classic MariaDB has different shape.
		sku, _ := p.Attributes["sku_name"].(string)
		return map[string]any{
			"service":       "MariaDB",
			"skuName":       sku,
			"armRegionName": region,
		}, nil
	case "azurerm_storage_account":
		return map[string]any{
			"service":       "Storage",
			"skuName":       "Standard LRS",
			"armRegionName": region,
			"tier":          "Standard",
			"meterName":     "LRS Data Stored",
		}, nil
	default:
		return nil, nil
	}
}

// regionFromID extracts the region from a primitive ID like
// "<component>.azure-<region>.<localname>".
func regionFromID(id string) string {
	for i := 0; i < len(id); i++ {
		if i+7 <= len(id) && id[i:i+7] == ".azure-" {
			rest := id[i+7:]
			for j := 0; j < len(rest); j++ {
				if rest[j] == '.' {
					return rest[:j]
				}
			}
			return rest
		}
	}
	return ""
}

func dbTierFromSKU(sku string) string {
	switch {
	case len(sku) > 11 && sku[:11] == "Standard_B1":
		return "Burstable"
	case len(sku) > 10 && sku[:10] == "Standard_B":
		return "Burstable"
	case len(sku) > 10 && sku[:10] == "Standard_D":
		return "GeneralPurpose"
	}
	return "GeneralPurpose"
}

