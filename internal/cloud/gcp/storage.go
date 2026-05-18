package gcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func emitStorageImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	_ = refs
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "storage"
	}
	project, _ := target.Spec["project"].(string)

	bucketName, _ := target.Spec["name"].(string)
	if bucketName == "" {
		bucketName = deriveBucketName(project, component, target.Region)
	}
	if len(bucketName) < 3 || len(bucketName) > 63 {
		return nil, fmt.Errorf("gcp.emitStorage: bucket name %q must be 3-63 chars (got %d)", bucketName, len(bucketName))
	}

	versioning := boolFromSpec(target.Spec, "versioning", true)
	publicAccess, _ := target.Spec["publicAccess"].(string)
	prevention := "enforced"
	if publicAccess == "allowed" {
		prevention = "inherited"
	}

	storageClass, _ := target.Spec["storageClass"].(string)
	if storageClass == "" {
		storageClass = "STANDARD"
	}

	name := tofuIdent(component)
	return []ir.ResourcePrimitive{
		{
			ID:           fmt.Sprintf("%s.gcp-%s.bucket", component, target.Region),
			Cloud:        "gcp",
			TofuType:     "google_storage_bucket",
			TofuName:     name,
			TagAttribute: "labels",
			Attributes: map[string]any{
				"name":                        bucketName,
				"location":                    strings.ToUpper(target.Region),
				"storage_class":               storageClass,
				"uniform_bucket_level_access": true,
				"public_access_prevention":    prevention,
				"force_destroy":               false,
				"versioning": []any{map[string]any{
					"enabled": versioning,
				}},
			},
		},
	}, nil
}

// deriveBucketName produces a GCS-legal bucket name (3-63 chars, lowercase
// letters / digits / hyphens) from project + component + region with a
// deterministic 6-char sha256 suffix to reduce global-namespace collisions.
func deriveBucketName(project, component, region string) string {
	parts := []string{}
	if project != "" {
		if s := sanitizeBucketPart(project); s != "" {
			parts = append(parts, s)
		}
	}
	if s := sanitizeBucketPart(component); s != "" {
		parts = append(parts, s)
	}
	if s := sanitizeBucketPart(region); s != "" {
		parts = append(parts, s)
	}
	base := strings.Join(parts, "-")
	if len(base) > 50 {
		base = base[:50]
	}
	base = strings.Trim(base, "-")
	sum := sha256.Sum256([]byte(component + ":" + region + ":" + project))
	suffix := hex.EncodeToString(sum[:])[:6]
	if base == "" {
		return "nimbusfab-" + suffix
	}
	return base + "-" + suffix
}

func sanitizeBucketPart(s string) string {
	out := strings.Builder{}
	for _, c := range strings.ToLower(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out.WriteRune(c)
		case c == '-' || c == '_':
			out.WriteRune('-')
		}
	}
	return strings.Trim(out.String(), "-")
}
