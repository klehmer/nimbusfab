package provisioner

import (
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

// buildResolvedRefs translates a component's declared cross-component refs
// into a ResolvedRefs map of tofu interpolation strings, keyed by the user's
// alias (`as:` in YAML). Adapters look these up by alias when emitting
// resource attributes; the value substitutes into the workspace JSON
// verbatim and is resolved by tofu at plan/apply time against the
// `variable` block written by the workspace renderer.
//
// Convention: when snake_case(alias) is the singular of the upstream output
// (e.g. alias `subnetId` for output `subnet_ids`), the interpolation
// subscripts `[0]` so a scalar consumer (compute's `subnet_id`) reads the
// first element. When snake_case(alias) equals the output verbatim (e.g.
// `subnetIds`/`subnet_ids`, `vpcId`/`vpc_id`), the bare interpolation is
// used and the consumer takes the whole value (list or scalar).
func buildResolvedRefs(refs []ir.ComponentRef) cloud.ResolvedRefs {
	out := cloud.ResolvedRefs{}
	for _, r := range refs {
		if r.As == "" || r.Component == "" || r.Output == "" {
			continue
		}
		varName := upstream.VarName(r.Component, r.Output)
		base := "var." + varName
		if camelToSnake(r.As)+"s" == r.Output {
			out[r.As] = "${" + base + "[0]}"
		} else {
			out[r.As] = "${" + base + "}"
		}
	}
	return out
}

func camelToSnake(s string) string {
	out := make([]byte, 0, len(s)+2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			if i > 0 {
				out = append(out, '_')
			}
			out = append(out, c+32)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
