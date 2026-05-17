package provisioner

import "github.com/klehmer/nimbusfab/pkg/ir"

// tagContext carries the values the provisioner injects as framework tags.
type tagContext struct {
	Component    string
	DeploymentID string
	OrgID        string
}

// injectFrameworkTags returns a copy of p with infra:* tags merged in.
// User-provided tags take precedence ONLY for non-infra:* keys; framework
// keys are always overwritten with the framework value (this is how the
// inventory join works reliably).
func injectFrameworkTags(p ir.ResourcePrimitive, c tagContext) ir.ResourcePrimitive {
	out := p
	if out.NoTags {
		return out
	}
	copyTags := make(map[string]string, len(out.Tags)+3)
	for k, v := range out.Tags {
		copyTags[k] = v
	}
	out.Tags = copyTags
	if c.Component != "" {
		out.Tags["infra:component"] = c.Component
	}
	if c.DeploymentID != "" {
		out.Tags["infra:deployment_id"] = c.DeploymentID
	}
	orgID := c.OrgID
	if orgID == "" {
		orgID = "local"
	}
	out.Tags["infra:org_id"] = orgID
	return out
}
