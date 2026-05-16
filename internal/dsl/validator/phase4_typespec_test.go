package validator

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func newReport() *ir.ValidationReport { return &ir.ValidationReport{} }

func componentWithSpec(name, typ string, spec map[string]any) ir.Component {
	return ir.Component{
		Name:    name,
		Type:    typ,
		Spec:    spec,
		Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
	}
}

func issueCodes(rep *ir.ValidationReport) []string {
	out := make([]string, 0, len(rep.Issues))
	for _, i := range rep.Issues {
		out = append(out, i.Code)
	}
	return out
}

func TestPhase4_HappyPath(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("net", "network", map[string]any{"cidr": "10.0.0.0/16"}),
		},
	}
	rep := newReport()
	if err := phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep); err != nil {
		t.Fatalf("phase4: %v", err)
	}
	if len(rep.Issues) != 0 {
		t.Errorf("expected no issues, got %v", rep.Issues)
	}
}

func TestPhase4_MissingRequiredField(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("net", "network", map[string]any{"subnetCount": 2}),
		},
	}
	rep := newReport()
	_ = phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) == 0 {
		t.Fatal("expected required-field issue")
	}
	found := false
	for _, i := range rep.Issues {
		if i.Code == "ErrValidatorTypeSpec" && strings.Contains(i.Path, "components[0].spec") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ErrValidatorTypeSpec at components[0].spec, got %+v", rep.Issues)
	}
}

func TestPhase4_WrongTypeValue(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("net", "network", map[string]any{"cidr": "10.0.0.0/16", "subnetCount": "two"}),
		},
	}
	rep := newReport()
	_ = phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) == 0 {
		t.Fatal("expected wrong-type issue")
	}
	if rep.Issues[0].Code != "ErrValidatorTypeSpec" {
		t.Errorf("code = %q", rep.Issues[0].Code)
	}
	if !strings.Contains(rep.Issues[0].Path, "subnetCount") {
		t.Errorf("path = %q (want substring subnetCount)", rep.Issues[0].Path)
	}
}

func TestPhase4_UnknownType(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("typo", "storrage", map[string]any{}),
		},
	}
	rep := newReport()
	_ = phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(rep.Issues), rep.Issues)
	}
	if rep.Issues[0].Code != "ErrValidatorUnknownType" {
		t.Errorf("code = %q", rep.Issues[0].Code)
	}
	if rep.Issues[0].Path != "components[0].type" {
		t.Errorf("path = %q", rep.Issues[0].Path)
	}
	if !strings.Contains(rep.Issues[0].Message, "storrage") {
		t.Errorf("message missing user input: %q", rep.Issues[0].Message)
	}
}

func TestPhase4_BadCIDRPattern(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("net", "network", map[string]any{"cidr": "not-a-cidr"}),
		},
	}
	rep := newReport()
	_ = phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) == 0 {
		t.Fatal("expected pattern violation issue")
	}
	codes := issueCodes(rep)
	hasTypeSpec := false
	for _, c := range codes {
		if c == "ErrValidatorTypeSpec" {
			hasTypeSpec = true
		}
	}
	if !hasTypeSpec {
		t.Errorf("expected ErrValidatorTypeSpec, got %v", codes)
	}
}

func TestPhase4_SchemaCachingWithinInvocation(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("a", "network", map[string]any{"cidr": "10.0.0.0/16"}),
			componentWithSpec("b", "network", map[string]any{"cidr": "10.1.0.0/16"}),
			componentWithSpec("c", "network", map[string]any{"cidr": "10.2.0.0/16"}),
		},
	}
	rep := newReport()
	if err := phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep); err != nil {
		t.Fatalf("phase4: %v", err)
	}
	if len(rep.Issues) != 0 {
		t.Errorf("unexpected issues: %v", rep.Issues)
	}
}

func TestPhase4_NilRegistry(t *testing.T) {
	proj := &ir.Project{APIVersion: ir.APIVersionV1Alpha1, Name: "p"}
	rep := newReport()
	if err := phase4TypeSpecImpl(proj, nil, rep); err == nil {
		t.Error("expected error for nil registry")
	}
}

func TestPhase4_EmptySpec(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("net", "network", nil),
		},
	}
	rep := newReport()
	_ = phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep)
	if len(rep.Issues) == 0 {
		t.Fatal("expected required-field issue for empty spec (cidr missing)")
	}
}

func TestPhase4_MultipleComponentsMultipleErrors(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "p",
		Components: []ir.Component{
			componentWithSpec("net1", "network", map[string]any{}),       // missing cidr
			componentWithSpec("net2", "network", map[string]any{"cidr": "bad"}), // bad pattern
			componentWithSpec("net3", "network", map[string]any{"cidr": "10.0.0.0/16"}),
		},
	}
	rep := newReport()
	_ = phase4TypeSpecImpl(proj, components.DefaultRegistry(), rep)
	hits := map[string]int{}
	for _, i := range rep.Issues {
		if strings.Contains(i.Path, "components[0]") {
			hits["c0"]++
		}
		if strings.Contains(i.Path, "components[1]") {
			hits["c1"]++
		}
		if strings.Contains(i.Path, "components[2]") {
			hits["c2"]++
		}
	}
	if hits["c0"] == 0 || hits["c1"] == 0 {
		t.Errorf("phase 4 aborted early; hits = %v issues = %v", hits, rep.Issues)
	}
	if hits["c2"] != 0 {
		t.Errorf("unexpected issues on valid component c2: %v", rep.Issues)
	}
}
