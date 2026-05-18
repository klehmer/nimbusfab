package graph

import (
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

// FromIR builds a graph.Input's Components + PairingErrors fields from
// IR-side types. Callers fill Targets and Direction separately.
//
// Pure function; keeps adapter boilerplate out of the CLI and webapi
// call sites, which both consume pkg/graph and produce these conversions.
func FromIR(components []ir.Component, pairErrors []upstream.PairingError) ([]Component, []PairingError) {
	gComps := make([]Component, len(components))
	for i, c := range components {
		refs := make([]Ref, len(c.Refs))
		for j, r := range c.Refs {
			refs[j] = Ref{Component: r.Component, Output: r.Output, As: r.As}
		}
		gComps[i] = Component{Name: c.Name, Type: c.Type, Refs: refs}
	}
	gPairs := make([]PairingError, len(pairErrors))
	for i, pe := range pairErrors {
		gPairs[i] = PairingError{
			Component: pe.Component,
			Ref:       Ref{Component: pe.Ref.Component, Output: pe.Ref.Output, As: pe.Ref.As},
			Cloud:     pe.Cloud, Region: pe.Region, Reason: pe.Reason,
		}
	}
	return gComps, gPairs
}
