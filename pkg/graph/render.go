package graph

import (
	"bytes"
	"fmt"
	"html"
)

// RenderSVG returns a self-contained SVG document with embedded styles.
// Suitable for inline-in-HTML (the styles use scoped selectors) and for
// standalone files (the document is valid by itself).
func RenderSVG(out *Output) []byte {
	if out == nil {
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0"></svg>`)
	}
	w := out.Width
	h := out.Height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 60
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" class="nimbusfab-graph">`, w, h)
	b.WriteString(`<style>
.nimbusfab-graph .graph-node rect { fill: #1e4d2b; stroke: #5cb85c; stroke-width: 1.5; }
.nimbusfab-graph .graph-node text { fill: #e8e8e8; font-family: system-ui, sans-serif; font-size: 12px; }
.nimbusfab-graph .graph-node .type { fill: #9cc; font-size: 10px; }
.nimbusfab-graph .edge-ok { stroke: #888; stroke-width: 1.5; fill: none; }
.nimbusfab-graph .edge-unmatched { stroke: #cc4444; stroke-width: 1.5; stroke-dasharray: 5,3; fill: none; }
.nimbusfab-graph .badge { font-size: 9px; }
.nimbusfab-graph .badge-applied { fill: #5cb85c; }
.nimbusfab-graph .badge-failed { fill: #cc4444; }
.nimbusfab-graph .badge-blocked { fill: #d9a73d; }
.nimbusfab-graph .badge-pending { fill: #888; }
</style>`)

	// Edges first so nodes draw on top.
	for _, e := range out.Edges {
		class := "edge-ok"
		if e.Kind == "unmatched" {
			class = "edge-unmatched"
		}
		fmt.Fprintf(&b, `<path d="%s" class="%s"/>`, html.EscapeString(e.D), class)
	}

	for _, n := range out.Nodes {
		fmt.Fprintf(&b, `<g class="graph-node" data-component="%s">`, html.EscapeString(n.Name))
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="4"/>`, n.X, n.Y, n.W, n.H)
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle">%s</text>`,
			n.X+n.W/2, n.Y+18, html.EscapeString(n.Name))
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" class="type">%s</text>`,
			n.X+n.W/2, n.Y+32, html.EscapeString(n.Type))
		for i, badge := range n.Badges {
			fmt.Fprintf(&b, `<text x="%d" y="%d" class="badge badge-%s">%s</text>`,
				n.X+10+i*38, n.Y+n.H-10,
				html.EscapeString(badge.Status), html.EscapeString(badge.Cloud))
		}
		b.WriteString(`</g>`)
	}

	b.WriteString(`</svg>`)
	return b.Bytes()
}
