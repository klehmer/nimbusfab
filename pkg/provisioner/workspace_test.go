package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestWriteWorkspace_EmitsVariableAndOutputBlocks(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives: []ir.ResourcePrimitive{{
			ID: "x.aws.vpc", Cloud: "aws", TofuType: "aws_vpc", TofuName: "net",
			Attributes: map[string]any{"cidr_block": "10.0.0.0/16"},
		}},
		Variables: []UpstreamVariable{
			{Name: "upstream_net_vpc_id", TofuType: "string"},
			{Name: "upstream_net_subnet_ids", TofuType: "list(string)"},
		},
		OutputBindings: map[string]any{
			"vpc_id":     "${aws_vpc.net.id}",
			"subnet_ids": []string{"${aws_subnet.net_0.id}"},
		},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.tf.json"))
	if err != nil {
		t.Fatalf("read main.tf.json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	variables, ok := parsed["variable"].(map[string]any)
	if !ok || len(variables) != 2 {
		t.Errorf("variable block: %v", parsed["variable"])
	}
	outputs, ok := parsed["output"].(map[string]any)
	if !ok || len(outputs) != 2 {
		t.Errorf("output block: %v", parsed["output"])
	}
	if _, present := parsed["data"]; present {
		t.Errorf("data block should be absent in v1.1 workspaces")
	}
}

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

func TestWriteWorkspace_OutputListIsJSONArray(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives:     []ir.ResourcePrimitive{{ID: "x", Cloud: "aws", TofuType: "aws_vpc", TofuName: "net", Attributes: map[string]any{}}},
		OutputBindings: map[string]any{
			"vpc_id":     "${aws_vpc.net.id}",
			"subnet_ids": []string{"${aws_subnet.net_0.id}", "${aws_subnet.net_1.id}"},
		},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "main.tf.json"))
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	outputs := parsed["output"].(map[string]any)
	subnetOut := outputs["subnet_ids"].(map[string]any)
	subnetValue, ok := subnetOut["value"].([]any)
	if !ok {
		t.Fatalf("subnet_ids value should be a JSON array, got %T: %v", subnetOut["value"], subnetOut["value"])
	}
	if len(subnetValue) != 2 || subnetValue[0] != "${aws_subnet.net_0.id}" {
		t.Errorf("unexpected subnet_ids value: %v", subnetValue)
	}
	vpcOut := outputs["vpc_id"].(map[string]any)
	if _, ok := vpcOut["value"].(string); !ok {
		t.Errorf("vpc_id should remain a string, got %T", vpcOut["value"])
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
