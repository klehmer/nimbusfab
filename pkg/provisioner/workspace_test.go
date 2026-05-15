package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestWriteWorkspace_AllFourFilesPresent(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives: []ir.ResourcePrimitive{{
			ID:         "web.aws-us-east-1.vpc",
			Cloud:      "aws",
			TofuType:   "aws_vpc",
			TofuName:   "web",
			Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
			Tags:       map[string]string{"infra:component": "web", "infra:org_id": "local"},
		}},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	for _, f := range []string{"versions.tf.json", "provider.tf.json", "backend.tf.json", "main.tf.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
}

func TestWriteWorkspace_MainTfJSONIsCanonical(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives: []ir.ResourcePrimitive{{
			TofuType:   "aws_vpc",
			TofuName:   "web",
			Attributes: map[string]any{"z_field": 1, "a_field": 2},
		}},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.tf.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("malformed JSON: %v", err)
	}
	canon, _ := canonicalJSON(v)
	if string(canon) != string(body) {
		t.Errorf("workspace JSON not canonical:\n got:  %s\n want: %s", body, canon)
	}
}

func TestWriteWorkspace_ByteIdenticalAcrossRuns(t *testing.T) {
	layout := func(dir string) WorkspaceLayout {
		return WorkspaceLayout{
			Dir:            dir,
			ProviderName:   "aws",
			ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
			Backend:        ir.StateBackend{Kind: "local"},
			Primitives: []ir.ResourcePrimitive{{
				TofuType:   "aws_vpc",
				TofuName:   "web",
				Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
				Tags:       map[string]string{"infra:component": "web"},
			}},
		}
	}
	a := t.TempDir()
	b := t.TempDir()
	if err := WriteWorkspace(layout(a)); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := WriteWorkspace(layout(b)); err != nil {
		t.Fatalf("b: %v", err)
	}
	for _, f := range []string{"main.tf.json", "provider.tf.json", "versions.tf.json", "backend.tf.json"} {
		ab, _ := os.ReadFile(filepath.Join(a, f))
		bb, _ := os.ReadFile(filepath.Join(b, f))
		if string(ab) != string(bb) {
			t.Errorf("%s differs between runs:\n a: %s\n b: %s", f, ab, bb)
		}
	}
}

func TestWriteWorkspace_ResourceBlockShape(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives: []ir.ResourcePrimitive{{
			TofuType:   "aws_vpc",
			TofuName:   "web",
			Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
			Tags:       map[string]string{"infra:component": "web"},
		}},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "main.tf.json"))
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("malformed JSON: %v", err)
	}
	resource, ok := parsed["resource"].(map[string]any)
	if !ok {
		t.Fatalf("missing resource key: %v", parsed)
	}
	awsVpc, ok := resource["aws_vpc"].(map[string]any)
	if !ok {
		t.Fatalf("missing aws_vpc key: %v", resource)
	}
	web, ok := awsVpc["web"].(map[string]any)
	if !ok {
		t.Fatalf("missing aws_vpc.web key: %v", awsVpc)
	}
	if web["cidr_block"] != "10.0.0.0/16" {
		t.Errorf("cidr_block = %v", web["cidr_block"])
	}
	tags, _ := web["tags"].(map[string]any)
	if tags["infra:component"] != "web" {
		t.Errorf("tags.infra:component = %v", tags["infra:component"])
	}
}
