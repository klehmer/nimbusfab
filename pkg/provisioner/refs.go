package provisioner

import (
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// UpstreamStateRef captures one cross-component reference: the dependent's
// workspace needs to read the upstream component's outputs via
// data.terraform_remote_state.
type UpstreamStateRef struct {
	Component string
	Backend   ir.StateBackend
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
