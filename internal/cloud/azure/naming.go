package azure

import (
	"regexp"
	"strings"
)

var azureAlnumRe = regexp.MustCompile(`[^a-z0-9]`)

// azureStorageAccountName returns a globally-unique-ish lowercase alphanum
// name 3-24 chars. Storage account names are globally unique in Azure; we
// disambiguate by appending the first 12 chars of the deployment-id prefix
// (everything after the "dep-" leader, lowercase, alphanum only).
func azureStorageAccountName(component, deploymentID string) string {
	base := azureAlnumRe.ReplaceAllString(strings.ToLower(component), "")
	suffix := strings.TrimPrefix(strings.ToLower(deploymentID), "dep-")
	suffix = azureAlnumRe.ReplaceAllString(suffix, "")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	maxBase := 24 - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	out := base + suffix
	if len(out) < 3 {
		out = out + strings.Repeat("0", 3-len(out))
	}
	return out
}

var azureCloudNameRe = regexp.MustCompile(`[^a-z0-9-]`)

// azureCloudResourceName returns a lowercase alphanum + hyphen name suitable
// for Cloud SQL flexible-server names and similar Azure resource attributes
// that disallow underscores and uppercase characters.
func azureCloudResourceName(component string) string {
	s := strings.ToLower(component)
	s = strings.ReplaceAll(s, "_", "-")
	s = azureCloudNameRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "resource"
	}
	return s
}
