package validator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// phase5RefsImpl validates the cross-component reference graph:
//   - each ref points at an existing component (ErrValidatorRefUnknownComponent)
//   - the referenced output is declared by the target's Type (ErrValidatorRefUnknownOutput)
//   - no component refs itself (ErrValidatorRefSelf)
//   - no cycles in the ref graph (ErrValidatorRefCycle)
func phase5RefsImpl(proj *ir.Project, registry components.Registry, report *ir.ValidationReport) error {
	if registry == nil {
		return fmt.Errorf("validator phase 5: nil registry")
	}

	nameIdx := make(map[string]int, len(proj.Components))
	knownNames := make([]string, 0, len(proj.Components))
	for i, c := range proj.Components {
		nameIdx[c.Name] = i
		knownNames = append(knownNames, c.Name)
	}

	for i, comp := range proj.Components {
		for j, ref := range comp.Refs {
			if ref.Component == comp.Name {
				report.Issues = append(report.Issues, ir.Issue{
					Severity: ir.SeverityError,
					Code:     "ErrValidatorRefSelf",
					Message:  fmt.Sprintf("component %q refs itself", comp.Name),
					Path:     fmt.Sprintf("components[%d].refs[%d].component", i, j),
				})
				continue
			}
			targetIdx, ok := nameIdx[ref.Component]
			if !ok {
				report.Issues = append(report.Issues, ir.Issue{
					Severity: ir.SeverityError,
					Code:     "ErrValidatorRefUnknownComponent",
					Message:  fmt.Sprintf("ref points at unknown component %q (known: %s)", ref.Component, strings.Join(knownNames, ", ")),
					Path:     fmt.Sprintf("components[%d].refs[%d].component", i, j),
				})
				continue
			}
			target := proj.Components[targetIdx]
			t, ok := registry.Type(target.Type)
			if !ok {
				// Phase 4 already flagged the target's bad type; suppress
				// noise here so the user fixes the type then re-runs.
				continue
			}
			outs := t.Outputs()
			if ref.Output == "" {
				report.Issues = append(report.Issues, ir.Issue{
					Severity: ir.SeverityError,
					Code:     "ErrValidatorRefUnknownOutput",
					Message:  fmt.Sprintf("component %q (type %s) ref has empty output name (declared: %s)", target.Name, target.Type, joinOutputNames(outs)),
					Path:     fmt.Sprintf("components[%d].refs[%d].output", i, j),
				})
				continue
			}
			if _, ok := outs[ref.Output]; !ok {
				report.Issues = append(report.Issues, ir.Issue{
					Severity: ir.SeverityError,
					Code:     "ErrValidatorRefUnknownOutput",
					Message:  fmt.Sprintf("component %q (type %s) does not declare output %q (declared: %s)", target.Name, target.Type, ref.Output, joinOutputNames(outs)),
					Path:     fmt.Sprintf("components[%d].refs[%d].output", i, j),
				})
			}
		}
	}

	detectCycles(proj, nameIdx, report)
	return nil
}

func joinOutputNames(outs map[string]components.OutputType) string {
	names := make([]string, 0, len(outs))
	for k := range outs {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// detectCycles runs three-color DFS over the ref graph. WHITE=0 (unvisited),
// GRAY=1 (in current DFS path), BLACK=2 (fully explored). A back-edge to a
// GRAY node closes a cycle; the path from that node back to the current node
// (via parent pointers) plus the closing edge is reported.
func detectCycles(proj *ir.Project, nameIdx map[string]int, report *ir.ValidationReport) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	n := len(proj.Components)
	color := make([]int, n)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = -1
	}
	reported := map[string]bool{}

	// Stable iteration order: walk components in declaration order.
	var dfs func(u int)
	dfs = func(u int) {
		color[u] = gray
		for _, ref := range proj.Components[u].Refs {
			v, ok := nameIdx[ref.Component]
			if !ok || v == u {
				continue
			}
			switch color[v] {
			case white:
				parent[v] = u
				dfs(v)
			case gray:
				emitCycle(proj, parent, u, v, reported, report)
			}
		}
		color[u] = black
	}

	for i := 0; i < n; i++ {
		if color[i] == white {
			dfs(i)
		}
	}
}

func emitCycle(proj *ir.Project, parent []int, u, v int, reported map[string]bool, report *ir.ValidationReport) {
	// Walk from u back to v via parent pointers; this gives the cycle nodes
	// in reverse order. Prepend each parent to assemble v→...→u.
	chain := []int{u}
	cur := u
	for cur != v {
		p := parent[cur]
		if p == -1 {
			break
		}
		chain = append([]int{p}, chain...)
		cur = p
		if cur == v {
			break
		}
	}
	if chain[0] != v {
		chain = append([]int{v}, chain...)
	}
	names := make([]string, 0, len(chain)+1)
	for _, idx := range chain {
		names = append(names, proj.Components[idx].Name)
	}
	names = append(names, proj.Components[v].Name)

	key := strings.Join(names, "→")
	if reported[key] {
		return
	}
	reported[key] = true

	report.Issues = append(report.Issues, ir.Issue{
		Severity: ir.SeverityError,
		Code:     "ErrValidatorRefCycle",
		Message:  fmt.Sprintf("ref cycle detected: %s", strings.Join(names, " → ")),
		Path:     fmt.Sprintf("components[%d].refs", chain[0]),
	})
}
