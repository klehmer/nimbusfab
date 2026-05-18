// Package upstream owns cross-component planning machinery: variable
// naming, topological ordering, dependent-to-upstream pairing, and
// extraction of upstream output values from tofu state.
package upstream

import (
	"errors"
	"fmt"
	"sort"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// VarName builds the tofu variable name for one (component, output) pair.
// Component names are sanitized with the same rules as workspace tofu local
// names (lowercase, alnum + underscore; leading non-alpha prefixed with '_').
func VarName(component, output string) string {
	return "upstream_" + sanitizeIdent(component) + "_" + output
}

func sanitizeIdent(s string) string {
	out := make([]byte, 0, len(s)+1)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 || (out[0] >= '0' && out[0] <= '9') {
		out = append([]byte{'_'}, out...)
	}
	return string(out)
}

// Toposort returns components in dependency-first order: for any ref A→B,
// B appears before A. Stable secondary sort on component name for
// determinism. Returns an error on cycle (validator Phase 5 should
// prevent that; this is an internal-error guard).
func Toposort(components []ir.Component) ([]ir.Component, error) {
	if len(components) == 0 {
		return nil, nil
	}

	byName := make(map[string]ir.Component, len(components))
	names := make([]string, 0, len(components))
	for _, c := range components {
		if _, exists := byName[c.Name]; exists {
			return nil, fmt.Errorf("upstream.Toposort: duplicate component name %q", c.Name)
		}
		byName[c.Name] = c
		names = append(names, c.Name)
	}
	sort.Strings(names)

	indegree := make(map[string]int, len(components))
	dependents := make(map[string][]string, len(components))
	for _, name := range names {
		indegree[name] = 0
	}
	for _, name := range names {
		comp := byName[name]
		for _, ref := range comp.Refs {
			if _, ok := byName[ref.Component]; !ok {
				continue
			}
			indegree[name]++
			dependents[ref.Component] = append(dependents[ref.Component], name)
		}
	}

	var ready []string
	for _, name := range names {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)

	out := make([]ir.Component, 0, len(components))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		out = append(out, byName[next])
		deps := append([]string{}, dependents[next]...)
		sort.Strings(deps)
		for _, d := range deps {
			indegree[d]--
			if indegree[d] == 0 {
				ready = append(ready, d)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(components) {
		return nil, fmt.Errorf("upstream.Toposort: cycle detected (placed %d of %d components)", len(out), len(components))
	}
	return out, nil
}

// PlanPlaceholders builds {varName: hcl-formatted-placeholder} for each ref
// declared on a dependent component. The component's upstream Type is looked
// up via the registry to determine each output's TofuType, then a structural
// placeholder of that type is encoded as an HCL literal suitable for a
// `tofu plan -var name=value` flag.
//
// Returned values are HCL literals: strings are double-quoted; lists are
// JSON-arrays-of-strings (which tofu accepts as list(string)); numbers and
// bools are bare tokens. Refs pointing at components not in `all` are
// silently dropped; the validator's Phase 5 catches structural errors.
func PlanPlaceholders(refs []ir.ComponentRef, all []ir.Component, reg components.Registry) (map[string]string, error) {
	out := map[string]string{}
	byName := map[string]ir.Component{}
	for _, c := range all {
		byName[c.Name] = c
	}
	for _, r := range refs {
		upstream, ok := byName[r.Component]
		if !ok {
			continue
		}
		typ, ok := reg.Type(upstream.Type)
		if !ok {
			continue
		}
		outDecl, ok := typ.Outputs()[r.Output]
		if !ok {
			continue
		}
		name := VarName(r.Component, r.Output)
		out[name] = placeholderFor(name, outDecl.TofuType())
	}
	return out, nil
}

// ErrCrossTargetRefUnsupported fires when a dependent target has no upstream
// target in the same (cloud, region). v1.1 explicitly does not support
// cross-cloud or cross-region refs.
var ErrCrossTargetRefUnsupported = errors.New("cross-target ref unsupported (no matching upstream target in same cloud/region)")

// TargetIdent is the (component, cloud, region) tuple uniquely identifying a
// deployment target for ordering and pairing purposes.
type TargetIdent struct {
	Component string
	Cloud     string
	Region    string
}

// Pair finds the upstream target matching dep's (cloud, region). Returns
// ErrCrossTargetRefUnsupported if no exact match exists.
func Pair(dep TargetIdent, upstream string, all []TargetIdent) (TargetIdent, error) {
	for _, t := range all {
		if t.Component == upstream && t.Cloud == dep.Cloud && t.Region == dep.Region {
			return t, nil
		}
	}
	return TargetIdent{}, fmt.Errorf("%w: %s in %s/%s needs %s",
		ErrCrossTargetRefUnsupported, dep.Component, dep.Cloud, dep.Region, upstream)
}

// ToposortTargets orders targets by (component-toposort-rank, cloud, region).
// All targets of an upstream component appear before any target of a
// downstream component, regardless of (cloud, region).
func ToposortTargets(targets []TargetIdent, comps []ir.Component) ([]TargetIdent, error) {
	ordered, err := Toposort(comps)
	if err != nil {
		return nil, err
	}
	rank := map[string]int{}
	for i, c := range ordered {
		rank[c.Name] = i
	}
	out := make([]TargetIdent, len(targets))
	copy(out, targets)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].Component], rank[out[j].Component]
		if ri != rj {
			return ri < rj
		}
		if out[i].Cloud != out[j].Cloud {
			return out[i].Cloud < out[j].Cloud
		}
		return out[i].Region < out[j].Region
	})
	return out, nil
}

func placeholderFor(varName, tofuType string) string {
	switch tofuType {
	case "string":
		return `"__nimbusfab_placeholder_` + varName + `__"`
	case "list(string)":
		return `["__nimbusfab_placeholder_` + varName + `_0__"]`
	case "number":
		return "0"
	case "bool":
		return "false"
	default:
		return `"__nimbusfab_placeholder_` + varName + `__"`
	}
}
