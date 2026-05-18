// Package upstream owns cross-component planning machinery: variable
// naming, topological ordering, dependent-to-upstream pairing, and
// extraction of upstream output values from tofu state.
package upstream

import (
	"fmt"
	"sort"

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
