package graph

import (
	"fmt"
	"sort"
)

const (
	nodeW   = 140
	nodeH   = 56
	gapX    = 32
	gapY    = 64
	marginX = 24
	marginY = 24
)

// Layout produces positioned nodes and routed edges for the given input.
// It accepts only "tb" or "lr" directions; an empty or unknown direction
// is treated as "tb". Returns an error if the input's refs form a cycle
// (validator Phase 5 should have caught that upstream).
func Layout(in Input) (*Output, error) {
	dir := in.DirectionOrDefault()
	if len(in.Components) == 0 {
		return &Output{Direction: dir}, nil
	}

	// Rank assignment via longest-path layering.
	ranks, err := assignRanks(in.Components)
	if err != nil {
		return nil, err
	}

	// Bucket components by rank, then sort within each rank by name for
	// deterministic columns.
	byRank := map[int][]Component{}
	for _, c := range in.Components {
		r := ranks[c.Name]
		byRank[r] = append(byRank[r], c)
	}
	maxRank := 0
	for r := range byRank {
		if r > maxRank {
			maxRank = r
		}
	}
	for r := range byRank {
		sort.Slice(byRank[r], func(i, j int) bool { return byRank[r][i].Name < byRank[r][j].Name })
	}

	// Coordinates: index within rank → column position; rank → row position.
	// For "lr" we swap rows and columns at the end.
	pos := map[string]NodeBox{}
	maxCols := 0
	for r := 0; r <= maxRank; r++ {
		col := byRank[r]
		if len(col) > maxCols {
			maxCols = len(col)
		}
		for i, c := range col {
			b := NodeBox{Name: c.Name, Type: c.Type, W: nodeW, H: nodeH,
				X:      marginX + i*(nodeW+gapX),
				Y:      marginY + r*(nodeH+gapY),
				Badges: badgesFor(c.Name, in.Targets),
			}
			pos[c.Name] = b
		}
	}

	// Apply direction transform if "lr".
	if dir == "lr" {
		for name, b := range pos {
			b.X, b.Y = b.Y, b.X
			pos[name] = b
		}
	}

	// Build nodes slice (deterministic order: by name).
	names := make([]string, 0, len(pos))
	for n := range pos {
		names = append(names, n)
	}
	sort.Strings(names)
	nodes := make([]NodeBox, 0, len(names))
	for _, n := range names {
		nodes = append(nodes, pos[n])
	}

	// Build edges, applying pairing-error annotation.
	unmatched := unmatchedEdges(in.PairingErrors)
	edges := make([]EdgePath, 0)
	for _, c := range in.Components {
		dst, ok := pos[c.Name]
		if !ok {
			continue
		}
		for _, r := range c.Refs {
			src, ok := pos[r.Component]
			if !ok {
				continue
			}
			kind := "ok"
			if _, bad := unmatched[edgeKey{From: r.Component, To: c.Name}]; bad {
				kind = "unmatched"
			}
			edges = append(edges, EdgePath{
				From: r.Component, To: c.Name,
				D:    routePath(src, dst, dir),
				Kind: kind,
			})
		}
	}

	// Bounding box.
	width := marginX*2 + maxCols*(nodeW+gapX) - gapX
	height := marginY*2 + (maxRank+1)*(nodeH+gapY) - gapY
	if dir == "lr" {
		width, height = height, width
	}

	return &Output{
		Width:     width,
		Height:    height,
		Direction: dir,
		Nodes:     nodes,
		Edges:     edges,
		HasErrors: len(in.PairingErrors) > 0,
	}, nil
}

func assignRanks(components []Component) (map[string]int, error) {
	byName := map[string]Component{}
	for _, c := range components {
		if _, dup := byName[c.Name]; dup {
			return nil, fmt.Errorf("graph.Layout: duplicate component %q", c.Name)
		}
		byName[c.Name] = c
	}

	rank := map[string]int{}
	visiting := map[string]bool{}

	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		if _, done := rank[name]; done {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("graph.Layout: cycle: %v", append(path, name))
		}
		visiting[name] = true
		max := -1
		comp, ok := byName[name]
		if ok {
			for _, r := range comp.Refs {
				if _, exists := byName[r.Component]; !exists {
					continue
				}
				if err := visit(r.Component, append(path, name)); err != nil {
					return err
				}
				if rank[r.Component] > max {
					max = rank[r.Component]
				}
			}
		}
		visiting[name] = false
		rank[name] = max + 1
		return nil
	}

	// Walk in stable order so test fixtures are deterministic.
	order := make([]string, 0, len(byName))
	for n := range byName {
		order = append(order, n)
	}
	sort.Strings(order)
	for _, n := range order {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return rank, nil
}

func badgesFor(component string, targets []TargetSnapshot) []TargetBadge {
	var out []TargetBadge
	for _, t := range targets {
		if t.Component == component {
			out = append(out, TargetBadge{Cloud: t.Cloud, Status: t.Status})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cloud < out[j].Cloud })
	return out
}

type edgeKey struct{ From, To string }

func unmatchedEdges(errors []PairingError) map[edgeKey]struct{} {
	out := map[edgeKey]struct{}{}
	for _, e := range errors {
		out[edgeKey{From: e.Ref.Component, To: e.Component}] = struct{}{}
	}
	return out
}

// routePath returns an SVG "d" attribute for an orthogonal edge from
// src to dst, given the layout direction.
func routePath(src, dst NodeBox, dir string) string {
	if dir == "lr" {
		// Source right-edge → dest left-edge.
		x1, y1 := src.X+src.W, src.Y+src.H/2
		x2, y2 := dst.X, dst.Y+dst.H/2
		midX := (x1 + x2) / 2
		return fmt.Sprintf("M %d %d L %d %d L %d %d L %d %d", x1, y1, midX, y1, midX, y2, x2, y2)
	}
	// "tb": source bottom-center → dest top-center.
	x1, y1 := src.X+src.W/2, src.Y+src.H
	x2, y2 := dst.X+dst.W/2, dst.Y
	midY := (y1 + y2) / 2
	return fmt.Sprintf("M %d %d L %d %d L %d %d L %d %d", x1, y1, x1, midY, x2, midY, x2, y2)
}
