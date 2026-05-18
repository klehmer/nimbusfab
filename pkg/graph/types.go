// Package graph builds positioned node + edge layouts for nimbusfab
// component dependency graphs and renders them as inline SVG. Used by
// the webapi (/ui/.../graph pages) and the `nimbusfab graph` CLI.
//
// The layout algorithm is a Sugiyama-lite: longest-path rank assignment +
// stable alphabetical column ordering + orthogonal edge routing. Adequate
// for typical nimbusfab project sizes (<50 components); crossing-reduction
// is deferred to a future revision.
package graph

// Input describes one graph the renderer should lay out.
type Input struct {
	// Components carries each component's name, type, and refs.
	Components []Component
	// Targets, if non-empty, drives per-component status badges.
	Targets []TargetSnapshot
	// PairingErrors, if non-empty, causes the matching refs to render
	// as red dashed edges.
	PairingErrors []PairingError
	// Direction is "tb" (top-down, default) or "lr" (left-right).
	Direction string
}

// DirectionOrDefault returns "tb" or "lr"; any other value (including
// empty) falls back to "tb".
func (i Input) DirectionOrDefault() string {
	if i.Direction == "lr" {
		return "lr"
	}
	return "tb"
}

// Component is one node in the graph.
type Component struct {
	Name string
	Type string
	Refs []Ref
}

// Ref is a typed reference from a dependent component to an upstream
// output. Same shape as ir.ComponentRef but kept local so pkg/graph has
// no import cycle with ir.
type Ref struct {
	Component string
	Output    string
	As        string
}

// TargetSnapshot is one (component, cloud, region, status) tuple used
// for status overlays. Status values match the inventory convention
// ("applied" / "failed" / "blocked" / "pending" / "destroyed" / etc.).
type TargetSnapshot struct {
	Component string
	Cloud     string
	Region    string
	Status    string
}

// PairingError marks one ref that has no matching upstream target in
// the dependent's (cloud, region). The renderer draws it as a red
// dashed edge; the provisioner uses the same struct for fail-fast.
type PairingError struct {
	Component string
	Ref       Ref
	Cloud     string
	Region    string
	Reason    string
}

// Output is what Layout produces and RenderSVG consumes.
type Output struct {
	Width     int
	Height    int
	Direction string
	Nodes     []NodeBox
	Edges     []EdgePath
	HasErrors bool
}

// NodeBox is one positioned component card.
type NodeBox struct {
	Name       string
	Type       string
	X, Y, W, H int
	Badges     []TargetBadge
}

// TargetBadge is one cloud-status indicator under a node label.
type TargetBadge struct {
	Cloud  string
	Status string
}

// EdgePath is one ref edge as an SVG path data string.
type EdgePath struct {
	From string // source component name
	To   string // dependent component name
	D    string // SVG <path d="…"> value
	Kind string // "ok" or "unmatched"
}
