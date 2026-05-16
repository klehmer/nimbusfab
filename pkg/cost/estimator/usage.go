package estimator

// HoursPerMonth is the assumed billing hours for monthly compute / DB
// estimates. 24 hours × 30.4375 days ≈ 730.5; rounded to 730.
const HoursPerMonth = 730.0

// DefaultStorageGB is the assumed bucket size for S3 estimates when the user
// hasn't supplied spec.usage.storageGB.
const DefaultStorageGB = 100.0

// UnitsFor returns the multiplier to apply to a primitive's unit price for one
// month's estimated cost, given any user-provided usage overrides.
//
// Returns 0 for primitives without a documented usage assumption (the
// estimator skips these with a warning).
func UnitsFor(tofuType string, usage map[string]any) float64 {
	switch tofuType {
	case "aws_instance", "aws_db_instance",
		"azurerm_linux_virtual_machine",
		"azurerm_postgresql_flexible_server",
		"azurerm_mysql_flexible_server",
		"azurerm_mariadb_server":
		if hr, ok := numberFrom(usage["hoursPerMonth"]); ok {
			return hr
		}
		return HoursPerMonth
	case "aws_s3_bucket", "azurerm_storage_account":
		if gb, ok := numberFrom(usage["storageGB"]); ok {
			return gb
		}
		return DefaultStorageGB
	default:
		return 0
	}
}

func numberFrom(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	}
	return 0, false
}
