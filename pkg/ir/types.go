// Package ir defines the Intermediate Representation that flows between every
// subsystem of nimbusfab. The YAML loader produces an *ir.Project;
// the validator checks and expands it; the provisioner walks it and asks cloud
// adapters to emit ResourcePrimitives; the cost estimator walks it for pricing
// keys. Everything downstream agrees on the types in this package.
//
// The IR is a public contract for plugin authors. Add fields additively within
// an APIVersion; introduce a new APIVersion for breaking changes.
package ir

// APIVersionV1Alpha1 is the current IR contract version. Pre-1.0 we are alpha.
const APIVersionV1Alpha1 = "infra.dev/v1alpha1"

// TargetMode controls how a Component fans out across clouds.
type TargetMode string

const (
	// ModeReplicate produces independent DeploymentTargets per cloud, each with
	// its own OpenTofu workspace and state. This is the v1 default.
	ModeReplicate TargetMode = "replicate"

	// ModeComposed produces a single logical target whose ResourcePrimitives
	// may span clouds linked by depends-on edges. v2 only; v1 engine rejects.
	ModeComposed TargetMode = "composed"
)

// Project is the top-level unit a user works on. One Project == one inventory
// scope (one row in the projects table) and lives in one directory of YAML.
type Project struct {
	APIVersion  string `json:"apiVersion" yaml:"apiVersion" jsonschema:"required,enum=infra.dev/v1alpha1"`
	Name        string `json:"name"       yaml:"name"       jsonschema:"required,pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$,maxLength=63"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Stacks declares the named environments for this Project. At least one
	// must be present. NOTE: invopop/jsonschema does not propagate
	// `minProperties` through to map-typed fields in the generated schema, so
	// the `>= 1 entry` invariant is enforced programmatically by the
	// validator's semantic-checks phase (DSL/IR Phase 2 plan), not by JSON
	// Schema.
	Stacks     map[string]Stack `json:"stacks" yaml:"stacks" jsonschema:"required,minProperties=1"`
	Components []Component      `json:"components,omitempty" yaml:"components,omitempty"`
	Comps      []Composition    `json:"compositions,omitempty" yaml:"compositions,omitempty"`
}

// Stack is a named environment within a Project (dev / staging / prod).
// Stack-level vars are merged into Component.Spec at validation time.
type Stack struct {
	Name         string         `json:"name"         yaml:"name"         jsonschema:"pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$,maxLength=63"`
	Vars         map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	StateBackend StateBackend   `json:"stateBackend,omitempty" yaml:"stateBackend,omitempty"`
}

// StateBackend selects how OpenTofu state is stored for this stack. The
// backend is one of the standard tofu backends (s3 / gcs / azurerm / pg /
// local). Concrete Config keys are backend-specific.
type StateBackend struct {
	Kind   string         `json:"kind"             yaml:"kind"             jsonschema:"required,enum=s3,enum=gcs,enum=azurerm,enum=pg,enum=local"`
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// Component is the logical thing the user declared. It may fan out to >1
// DeploymentTargets via the target shorthand (ModeReplicate) or be a single
// cross-cloud target (ModeComposed, v2).
type Component struct {
	Name    string             `json:"name"    yaml:"name"    jsonschema:"required,pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$,maxLength=63"`
	Type    string             `json:"type"    yaml:"type"    jsonschema:"required"`
	Mode    TargetMode         `json:"mode,omitempty" yaml:"mode,omitempty" jsonschema:"enum=replicate,enum=composed"`
	Spec    map[string]any     `json:"spec"    yaml:"spec"    jsonschema:"required"`
	Targets []DeploymentTarget `json:"targets" yaml:"targets" jsonschema:"required,minItems=1"`
	Refs    []ComponentRef     `json:"refs,omitempty" yaml:"refs,omitempty"`
	Policy  ComponentPolicy    `json:"policy,omitempty" yaml:"policy,omitempty"`
}

// ComponentPolicy carries deployment-level flags affecting the orchestrator.
type ComponentPolicy struct {
	// Serial forces targets in this Component to run one at a time instead of
	// the default parallel fan-out.
	Serial bool `json:"serial,omitempty" yaml:"serial,omitempty"`
}

// ComponentRef is a reference from one Component to another. Used by adapters
// to resolve outputs across components (e.g. a database referencing a
// network's VPC id).
type ComponentRef struct {
	Component string `json:"component" yaml:"component" jsonschema:"required"`
	Output    string `json:"output,omitempty" yaml:"output,omitempty"`
	As        string `json:"as,omitempty"     yaml:"as,omitempty"`
}

// DeploymentTarget is the concrete (Component, Cloud, Region, CredentialRef)
// tuple. The cloud adapter populates Primitives at plan time.
type DeploymentTarget struct {
	Cloud         string              `json:"cloud"          yaml:"cloud"          jsonschema:"required,pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$,maxLength=63"`
	Region        string              `json:"region"         yaml:"region"         jsonschema:"required"`
	CredentialRef string              `json:"credentialRef"  yaml:"credentialRef"  jsonschema:"required"`
	Spec          map[string]any      `json:"spec,omitempty" yaml:"spec,omitempty"`
	Primitives    []ResourcePrimitive `json:"primitives,omitempty" yaml:"-"`
}

// ResourcePrimitive is the unit a cloud adapter emits. It maps 1:1 to a
// resource block in the OpenTofu JSON configuration syntax that tofu-runner
// writes to disk.
type ResourcePrimitive struct {
	ID         string            `json:"id"` // "<component>.<target>.<localname>"
	Cloud      string            `json:"cloud"`
	TofuType   string            `json:"tofuType"` // e.g. "aws_db_instance"
	TofuName   string            `json:"tofuName"`
	Attributes map[string]any    `json:"attributes"` // raw tofu JSON body
	DependsOn  []string          `json:"dependsOn,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	// TagAttribute selects how framework tags attach to this primitive:
	//   ""        per-cloud default — "tags" on AWS/Azure, "" (skip) on GCP
	//   "tags"    AWS / Azure convention
	//   "labels"  GCP convention (stricter key/value rules; injectFrameworkTags
	//             sanitizes values for the [a-z0-9_-] + 63-char-cap constraint)
	// Resources that reject any tag/label attribute use the empty string AND
	// have no per-resource Tags set.
	TagAttribute string `json:"tagAttribute,omitempty"`
}

// Composition is a user-defined component type. At validation time, every
// Component whose Type matches the Composition's Kind is replaced with the
// expanded primitives. Expansion semantics are snapshot-per-deployment: edits
// to the Composition do not live-update existing deployments.
type Composition struct {
	APIVersion string         `json:"apiVersion" yaml:"apiVersion" jsonschema:"required,enum=infra.dev/v1alpha1"`
	Kind       string         `json:"kind"       yaml:"kind"       jsonschema:"required,pattern=^[A-Za-z][A-Za-z0-9]*$,maxLength=63"`
	Schema     map[string]any `json:"schema"     yaml:"schema"     jsonschema:"required"` // JSON Schema for the type's spec
	Template   CompositionTpl `json:"template"   yaml:"template"   jsonschema:"required"`
}

// CompositionTpl is the body of a Composition. Resources reference built-in
// component types and parameterize them from the consuming Component's spec.
type CompositionTpl struct {
	Resources []Component `json:"resources" yaml:"resources"`
}
