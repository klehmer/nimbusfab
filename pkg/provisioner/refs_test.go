package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestWriteWorkspace_WithUpstreamRef(t *testing.T) {
	dir := t.TempDir()
	layout := WorkspaceLayout{
		Dir:            dir,
		ProviderName:   "aws",
		ProviderConfig: map[string]any{"aws": map[string]any{"region": "us-east-1"}},
		Backend:        ir.StateBackend{Kind: "local"},
		Primitives: []ir.ResourcePrimitive{{
			TofuType: "aws_instance", TofuName: "app",
			Attributes: map[string]any{"subnet_id": "${data.terraform_remote_state.web_network.outputs.subnet_id}"},
		}},
		UpstreamRefs: []UpstreamStateRef{{
			Component: "web-network",
			Backend:   ir.StateBackend{Kind: "local", Config: map[string]any{"path": "/tmp/upstream.tfstate"}},
		}},
	}
	if err := WriteWorkspace(layout); err != nil {
		t.Fatalf("WriteWorkspace: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "main.tf.json"))
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data block: %s", body)
	}
	rs, ok := data["terraform_remote_state"].(map[string]any)
	if !ok {
		t.Fatalf("no terraform_remote_state block: %v", data)
	}
	if _, ok := rs["web_network"]; !ok {
		t.Errorf("missing remote state for web_network: %v", rs)
	}
}

func TestTofuIdentForComponent(t *testing.T) {
	cases := map[string]string{
		"web-network":   "web_network",
		"orders-db":     "orders_db",
		"WebApi":        "webapi",
		"123-bad":       "_123_bad",
		"":              "_",
		"good_name_42":  "good_name_42",
		"with.dots.bad": "with_dots_bad",
	}
	for in, want := range cases {
		got := tofuIdentForComponent(in)
		if got != want {
			t.Errorf("tofuIdentForComponent(%q) = %q, want %q", in, got, want)
		}
	}
}
