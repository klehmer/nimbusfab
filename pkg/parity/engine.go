package parity

import (
	"context"
	"fmt"
	"time"
)

// Engine is the public parity API.
type Engine interface {
	Compare(ctx context.Context, in CompareInput) (*ParityReport, error)
	EvaluateRules(ctx context.Context, report *ParityReport, rules ProjectRules) ([]Violation, error)
}

// CompareInput is what Compare takes.
type CompareInput struct {
	Component string
	Type      string
	Size      string
	Targets   []TargetProfile
}

// NewEngine returns a parity Engine seeded with the embedded contract catalog.
func NewEngine() (Engine, error) {
	contracts, err := LoadContracts()
	if err != nil {
		return nil, fmt.Errorf("parity.NewEngine: %w", err)
	}
	return &engine{contracts: contracts}, nil
}

type engine struct {
	contracts *Contracts
}

func (e *engine) Compare(ctx context.Context, in CompareInput) (*ParityReport, error) {
	report := &ParityReport{
		Component:   in.Component,
		Type:        in.Type,
		Size:        in.Size,
		Targets:     in.Targets,
		GeneratedAt: time.Now().UTC(),
	}
	if in.Size != "" {
		if floor, ok := e.contracts.Lookup(in.Type, in.Size); ok {
			report.Contract = floor
		}
	}
	report.Comparisons = BuildComparisons(in.Targets)
	report.Score = Score(report.Comparisons)
	if report.Contract.Type != "" {
		for _, t := range in.Targets {
			if w := floorWarnings(report.Contract, t); len(w) > 0 {
				report.Warnings = append(report.Warnings, w...)
			}
		}
	}
	return report, nil
}

func floorWarnings(floor ContractFloor, t TargetProfile) []string {
	var out []string
	label := t.Cloud + "/" + t.Region
	if floor.Compute != nil && t.Profile.Compute != nil {
		if t.Profile.Compute.VCPU < floor.Compute.MinVCPU {
			out = append(out, fmt.Sprintf("%s: compute.vCPU=%d below floor %d", label, t.Profile.Compute.VCPU, floor.Compute.MinVCPU))
		}
		if t.Profile.Compute.MemoryGB < floor.Compute.MinMemoryGB {
			out = append(out, fmt.Sprintf("%s: compute.memoryGB=%v below floor %v", label, t.Profile.Compute.MemoryGB, floor.Compute.MinMemoryGB))
		}
	}
	if floor.Compute != nil && t.Profile.Database != nil {
		if t.Profile.Database.Compute.VCPU < floor.Compute.MinVCPU {
			out = append(out, fmt.Sprintf("%s: database.compute.vCPU=%d below floor %d", label, t.Profile.Database.Compute.VCPU, floor.Compute.MinVCPU))
		}
		if t.Profile.Database.Compute.MemoryGB < floor.Compute.MinMemoryGB {
			out = append(out, fmt.Sprintf("%s: database.compute.memoryGB=%v below floor %v", label, t.Profile.Database.Compute.MemoryGB, floor.Compute.MinMemoryGB))
		}
	}
	return out
}
