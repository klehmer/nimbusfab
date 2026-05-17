package provisioner

import (
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// UpstreamStateRef captures one cross-component reference: the dependent's
// workspace needs to read the upstream component's outputs via
// data.terraform_remote_state.
type UpstreamStateRef struct {
	Component string
	Backend   ir.StateBackend
}

// buildResolvedRefs translates a component's declared cross-component refs
// into a ResolvedRefs map of tofu interpolation strings, keyed by the user's
// alias (`as:` in YAML). Adapters look these up by alias when emitting
// resource attributes; the value substitutes into the workspace JSON
// verbatim and is resolved by tofu at plan/apply time against the
// `data.terraform_remote_state.<referent>` block written by the workspace
// renderer.
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
		base := "data.terraform_remote_state." + tofuIdentForComponent(r.Component) + ".outputs." + r.Output
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

// tofuIdentForComponent matches the helper used in cloud adapters so the
// `data.terraform_remote_state.<name>` matches the upstream's local name.
func tofuIdentForComponent(name string) string {
	out := []byte{}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '-':
			out = append(out, '_')
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 || (out[0] >= '0' && out[0] <= '9') {
		out = append([]byte{'_'}, out...)
	}
	return string(out)
}
