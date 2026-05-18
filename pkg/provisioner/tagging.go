package provisioner

import (
	"regexp"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// tagContext carries the values the provisioner injects as framework tags.
type tagContext struct {
	Component    string
	DeploymentID string
	OrgID        string
}

// injectFrameworkTags attaches framework attribution (component, deployment,
// org) to the primitive's cloud-appropriate tag attribute. Per-cloud default:
// AWS / Azure → "tags"; GCP → "" (skip; explicit opt-in required via
// TagAttribute: "labels"). Resources that reject tag attributes set
// TagAttribute: "" AND don't populate Tags.
func injectFrameworkTags(p ir.ResourcePrimitive, ctx tagContext) ir.ResourcePrimitive {
	attr := resolveTagAttribute(p)
	if attr == "" {
		return p
	}
	merged := frameworkTags(ctx.Component, ctx.DeploymentID, ctx.OrgID)
	for k, v := range p.Tags {
		merged[k] = v
	}
	if attr == "labels" {
		merged = sanitizeForLabels(merged)
	}
	if p.Attributes == nil {
		p.Attributes = map[string]any{}
	}
	p.Attributes[attr] = merged
	p.Tags = nil
	return p
}

// resolveTagAttribute returns the key to write tags under in the resource's
// Attributes, or "" to skip tagging entirely.
//
//	""   (unset) → per-cloud default: "tags" on AWS/Azure, "" (skip) on GCP
//	"tags"       → explicit AWS/Azure tags key
//	"labels"     → explicit GCP labels key (stricter key/value rules)
//	"-"          → explicit skip — resource does not accept any tag attribute
func resolveTagAttribute(p ir.ResourcePrimitive) string {
	if p.TagAttribute == "-" {
		return ""
	}
	if p.TagAttribute != "" {
		return p.TagAttribute
	}
	if p.Cloud == "gcp" {
		return ""
	}
	return "tags"
}

var labelSanitizeRe = regexp.MustCompile(`[^a-z0-9_-]`)

// sanitizeForLabels normalizes a tag map for GCP labels: lowercase keys +
// values, [a-z0-9_-] only (replace anything else with '_'), 63-char cap.
func sanitizeForLabels(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		kk := labelSanitizeRe.ReplaceAllString(strings.ToLower(k), "_")
		if len(kk) > 63 {
			kk = kk[:63]
		}
		vv := labelSanitizeRe.ReplaceAllString(strings.ToLower(v), "_")
		if len(vv) > 63 {
			vv = vv[:63]
		}
		out[kk] = vv
	}
	return out
}
