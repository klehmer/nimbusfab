package yamlnode

import (
	"strings"
	"testing"
)

func TestDecode_CapturesProvenance(t *testing.T) {
	src := strings.TrimSpace(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`)
	doc, err := Decode("project.yaml", []byte(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if doc.APIVersion != "infra.dev/v1alpha1" {
		t.Errorf("APIVersion = %q", doc.APIVersion)
	}
	if doc.Name != "orders" {
		t.Errorf("Name = %q", doc.Name)
	}
	src1 := doc.SourceFor("apiVersion")
	if src1.File != "project.yaml" || src1.Line != 1 {
		t.Errorf("apiVersion source = %+v, want project.yaml:1", src1)
	}
	src2 := doc.SourceFor("name")
	if src2.Line != 2 {
		t.Errorf("name source = %+v, want line 2", src2)
	}
}

func TestDecode_Malformed(t *testing.T) {
	_, err := Decode("bad.yaml", []byte("apiVersion: : :\n  bad"))
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	yerr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if yerr.Source.File != "bad.yaml" {
		t.Errorf("Source.File = %q, want bad.yaml", yerr.Source.File)
	}
}
