package gcp

import (
	"regexp"
	"strings"
)

var gcpNameRe = regexp.MustCompile(`[^a-z0-9-]`)

// gcpResourceName returns a DNS-compliant name: lowercase alphanum + hyphens,
// 3-63 chars, must start with a lowercase letter, no trailing hyphen.
// Used for GCS bucket names (globally unique) and Cloud SQL instance names.
// Appends the deployment-id prefix to disambiguate.
func gcpResourceName(component, deploymentID string) string {
	base := strings.ToLower(component)
	base = strings.ReplaceAll(base, "_", "-")
	base = gcpNameRe.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" || !(base[0] >= 'a' && base[0] <= 'z') {
		base = "n" + base
	}

	suffix := strings.TrimPrefix(strings.ToLower(deploymentID), "dep-")
	suffix = gcpNameRe.ReplaceAllString(suffix, "")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}

	maxBase := 63 - len(suffix) - 1
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	out := base + "-" + suffix
	out = strings.TrimRight(out, "-")
	if len(out) < 3 {
		out = out + strings.Repeat("0", 3-len(out))
	}
	return out
}
