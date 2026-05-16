package validator

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func networkComp(name string, refs ...ir.ComponentRef) ir.Component {
	return ir.Component{
		Name:    name,
		Type:    "network",
		Spec:    map[string]any{"cidr": "10.0.0.0/16"},
		Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		Refs:    refs,
	}
}

func computeComp(name string, refs ...ir.ComponentRef) ir.Component {
	return ir.Component{
		Name:    name,
		Type:    "compute",
		Spec:    map[string]any{"size": "small"},
		Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		Refs:    refs,
	}
}

func TestPhase5_HappyPath(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			networkComp("web-net"),
			computeComp("web-app",
				ir.ComponentRef{Component: "web-net", Output: "vpc_id", As: "vpcId"},
				ir.ComponentRef{Component: "web-net", Output: "subnet_ids", As: "subnetIds"},
			),
		},
	}
	rep := newReport()
	if err := phase5RefsImpl(proj, components.DefaultRegistry(), rep); err != nil {
		t.Fatalf("phase5: %v", err)
	}
	if len(rep.Issues) != 0 {
		t.Errorf("expected no issues, got %v", rep.Issues)
	}
}

func TestPhase5_UnknownComponent(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			networkComp("web-net"),
			computeComp("web-app",
				ir.ComponentRef{Component: "webnetwork", Output: "vpc_id"},
			),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	if rep.Issues[0].Code != "ErrValidatorRefUnknownComponent" {
		t.Errorf("code = %q", rep.Issues[0].Code)
	}
	if rep.Issues[0].Path != "components[1].refs[0].component" {
		t.Errorf("path = %q", rep.Issues[0].Path)
	}
	if !strings.Contains(rep.Issues[0].Message, "webnetwork") {
		t.Errorf("message missing typo input: %q", rep.Issues[0].Message)
	}
}

func TestPhase5_UnknownOutput(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			computeComp("web-app"),
			networkComp("orders-db",
				ir.ComponentRef{Component: "web-app", Output: "subnet_ids", As: "subnetIds"},
			),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	if rep.Issues[0].Code != "ErrValidatorRefUnknownOutput" {
		t.Errorf("code = %q", rep.Issues[0].Code)
	}
	if !strings.Contains(rep.Issues[0].Message, "instance_ids") {
		t.Errorf("message should list declared outputs, got: %q", rep.Issues[0].Message)
	}
}

func TestPhase5_SelfRef(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			computeComp("web-app",
				ir.ComponentRef{Component: "web-app", Output: "instance_ids"},
			),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	if rep.Issues[0].Code != "ErrValidatorRefSelf" {
		t.Errorf("code = %q", rep.Issues[0].Code)
	}
	if rep.Issues[0].Path != "components[0].refs[0].component" {
		t.Errorf("path = %q", rep.Issues[0].Path)
	}
}

func TestPhase5_CycleLength2(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			computeComp("a", ir.ComponentRef{Component: "b", Output: "instance_ids"}),
			computeComp("b", ir.ComponentRef{Component: "a", Output: "instance_ids"}),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	hasCycle := false
	for _, i := range rep.Issues {
		if i.Code == "ErrValidatorRefCycle" {
			hasCycle = true
			if !strings.Contains(i.Message, "a") || !strings.Contains(i.Message, "b") {
				t.Errorf("cycle message missing component names: %q", i.Message)
			}
		}
	}
	if !hasCycle {
		t.Errorf("expected ErrValidatorRefCycle in %+v", rep.Issues)
	}
}

func TestPhase5_CycleLength3(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			computeComp("a", ir.ComponentRef{Component: "b", Output: "instance_ids"}),
			computeComp("b", ir.ComponentRef{Component: "c", Output: "instance_ids"}),
			computeComp("c", ir.ComponentRef{Component: "a", Output: "instance_ids"}),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	hasCycle := false
	for _, i := range rep.Issues {
		if i.Code == "ErrValidatorRefCycle" {
			hasCycle = true
			for _, n := range []string{"a", "b", "c"} {
				if !strings.Contains(i.Message, n) {
					t.Errorf("cycle message missing %q: %q", n, i.Message)
				}
			}
		}
	}
	if !hasCycle {
		t.Errorf("expected ErrValidatorRefCycle in %+v", rep.Issues)
	}
}

func TestPhase5_TargetHasBadType(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			{
				Name:    "weird",
				Type:    "nonexistent-type",
				Spec:    map[string]any{},
				Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
			},
			computeComp("consumer",
				ir.ComponentRef{Component: "weird", Output: "something"},
			),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	// Phase 4 owns the unknown-type error; Phase 5 should not emit ANY
	// issue for the consumer (target name resolved, output check skipped).
	for _, i := range rep.Issues {
		if strings.HasPrefix(i.Code, "ErrValidatorRef") {
			t.Errorf("unexpected ref issue: %+v", i)
		}
	}
}

func TestPhase5_EmptyOutputField(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			networkComp("web-net"),
			computeComp("web-app", ir.ComponentRef{Component: "web-net", Output: ""}),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	if rep.Issues[0].Code != "ErrValidatorRefUnknownOutput" {
		t.Errorf("code = %q", rep.Issues[0].Code)
	}
	if !strings.Contains(rep.Issues[0].Message, "empty") {
		t.Errorf("message should mention empty: %q", rep.Issues[0].Message)
	}
}

func TestPhase5_NilRegistry(t *testing.T) {
	proj := &ir.Project{APIVersion: ir.APIVersionV1Alpha1, Name: "p"}
	rep := newReport()
	if err := phase5RefsImpl(proj, nil, rep); err == nil {
		t.Error("expected error for nil registry")
	}
}

func TestPhase5_MultipleErrorsPerComponent(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			networkComp("web-net"),
			computeComp("web-app",
				ir.ComponentRef{Component: "missing1", Output: "foo"},
				ir.ComponentRef{Component: "web-net", Output: "not_a_real_output"},
				ir.ComponentRef{Component: "missing2", Output: "bar"},
			),
		},
	}
	rep := newReport()
	_ = phase5RefsImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) != 3 {
		t.Fatalf("expected 3 issues, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	codes := map[string]int{}
	for _, i := range rep.Issues {
		codes[i.Code]++
	}
	if codes["ErrValidatorRefUnknownComponent"] != 2 {
		t.Errorf("expected 2 unknown-component issues, got %v", codes)
	}
	if codes["ErrValidatorRefUnknownOutput"] != 1 {
		t.Errorf("expected 1 unknown-output issue, got %v", codes)
	}
}
