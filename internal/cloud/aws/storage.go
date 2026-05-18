package aws

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func emitStorageImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "storage"
	}
	name := tofuIdentifier(component)

	bucketName, _ := target.Spec["name"].(string)
	if bucketName == "" {
		bucketName = deriveBucketName(component, target.Region)
	}

	versioning := boolFromSpec(target.Spec, "versioning", true)
	publicAccess, _ := target.Spec["publicAccess"].(string)
	blockPublic := publicAccess != "allowed"

	encAlgo := "AES256"
	if enc, ok := target.Spec["encryption"].(map[string]any); ok {
		if a, ok := enc["algorithm"].(string); ok && a != "" {
			encAlgo = a
		}
	}

	return []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.aws-%s.bucket", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_s3_bucket",
			TofuName: name,
			Attributes: map[string]any{
				"bucket": bucketName,
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.versioning", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_s3_bucket_versioning",
			TofuName: name,
			TagAttribute: "",
			Attributes: map[string]any{
				"bucket": "${aws_s3_bucket." + name + ".id}",
				"versioning_configuration": []any{map[string]any{
					"status": versioningStatus(versioning),
				}},
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.public_access_block", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_s3_bucket_public_access_block",
			TofuName: name,
			TagAttribute: "",
			Attributes: map[string]any{
				"bucket":                  "${aws_s3_bucket." + name + ".id}",
				"block_public_acls":       blockPublic,
				"block_public_policy":     blockPublic,
				"ignore_public_acls":      blockPublic,
				"restrict_public_buckets": blockPublic,
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.encryption", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_s3_bucket_server_side_encryption_configuration",
			TofuName: name,
			TagAttribute: "",
			Attributes: map[string]any{
				"bucket": "${aws_s3_bucket." + name + ".id}",
				"rule": []any{map[string]any{
					"apply_server_side_encryption_by_default": []any{map[string]any{
						"sse_algorithm": encAlgo,
					}},
				}},
			},
		},
	}, nil
}

func versioningStatus(enabled bool) string {
	if enabled {
		return "Enabled"
	}
	return "Suspended"
}

// deriveBucketName produces a deterministic, S3-legal bucket name from the
// component name + region. Format: <component-dashes>-<8-char-sha256-prefix>.
func deriveBucketName(component, region string) string {
	sum := sha256.Sum256([]byte(component + ":" + region))
	suffix := hex.EncodeToString(sum[:])[:8]
	safe := tofuIdentifier(component)
	out := ""
	for _, c := range safe {
		if c == '_' {
			out += "-"
		} else {
			out += string(c)
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return out + "-" + suffix
}
