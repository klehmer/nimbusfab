package provisioner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// WorkspaceLayout describes everything needed to materialize one
// DeploymentTarget's workspace on disk.
type WorkspaceLayout struct {
	Dir string

	ProviderName            string
	ProviderSource          string
	ProviderVersion         string
	ProviderRequiredVersion string
	ProviderConfig          map[string]any

	Backend ir.StateBackend

	Primitives []ir.ResourcePrimitive

	// Variables declares the typed tofu input variables this workspace
	// expects. The provisioner passes values via `tofu plan -var name=...`;
	// at plan time these are placeholders, at apply time real values from
	// upstream state.
	Variables []UpstreamVariable

	// OutputBindings declares the tofu expressions for outputs this
	// workspace publishes so its terraform.tfstate contains them after
	// apply. Keys are output names per components.Type.Outputs(); values
	// are HCL expressions written verbatim inside an `output {}` block.
	OutputBindings map[string]any
}

// UpstreamVariable is one tofu `variable` block declaration.
type UpstreamVariable struct {
	Name     string
	TofuType string
}

// WriteWorkspace materializes the four canonical workspace files atomically
// into layout.Dir. Files are written via tmpfile + rename; layout.Dir is
// created with mode 0700 if missing.
func WriteWorkspace(layout WorkspaceLayout) error {
	if err := os.MkdirAll(layout.Dir, 0o700); err != nil {
		return fmt.Errorf("workspace mkdir: %w", err)
	}

	mainBlock := buildMain(layout.Primitives)
	if len(layout.Variables) > 0 {
		vars := map[string]any{}
		for _, v := range layout.Variables {
			vars[v.Name] = map[string]any{"type": v.TofuType}
		}
		mainBlock["variable"] = vars
	}
	if len(layout.OutputBindings) > 0 {
		outs := map[string]any{}
		for name, expr := range layout.OutputBindings {
			outs[name] = map[string]any{"value": expr}
		}
		mainBlock["output"] = outs
	}

	files := map[string]any{
		"versions.tf.json": buildVersions(layout),
		"provider.tf.json": map[string]any{"provider": layout.ProviderConfig},
		"backend.tf.json":  buildBackend(layout.Backend),
		"main.tf.json":     mainBlock,
	}

	for name, content := range files {
		bytes, err := canonicalJSON(content)
		if err != nil {
			return fmt.Errorf("workspace %s: serialize: %w", name, err)
		}
		if err := atomicWrite(filepath.Join(layout.Dir, name), bytes); err != nil {
			return fmt.Errorf("workspace %s: %w", name, err)
		}
	}
	return nil
}

func buildVersions(layout WorkspaceLayout) map[string]any {
	required := layout.ProviderRequiredVersion
	if required == "" {
		required = ">= 1.7.0"
	}
	src := layout.ProviderSource
	if src == "" {
		src = "hashicorp/" + layout.ProviderName
	}
	ver := layout.ProviderVersion
	if ver == "" {
		ver = "~> 5.0"
	}
	return map[string]any{
		"terraform": map[string]any{
			"required_version":   required,
			"required_providers": map[string]any{layout.ProviderName: map[string]any{"source": src, "version": ver}},
		},
	}
}

func buildBackend(b ir.StateBackend) map[string]any {
	config := b.Config
	if config == nil {
		config = map[string]any{}
	}
	kind := b.Kind
	if kind == "" {
		kind = "local"
	}
	return map[string]any{
		"terraform": map[string]any{
			"backend": map[string]any{kind: config},
		},
	}
}

func buildMain(primitives []ir.ResourcePrimitive) map[string]any {
	sorted := make([]ir.ResourcePrimitive, len(primitives))
	copy(sorted, primitives)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	resource := map[string]any{}
	for _, p := range sorted {
		attrs := map[string]any{}
		for k, v := range p.Attributes {
			attrs[k] = v
		}
		if len(p.Tags) > 0 {
			tagMap := map[string]any{}
			for k, v := range p.Tags {
				tagMap[k] = v
			}
			attrs["tags"] = tagMap
		}
		if len(p.DependsOn) > 0 {
			dep := append([]string{}, p.DependsOn...)
			sort.Strings(dep)
			attrs["depends_on"] = dep
		}
		bucket, ok := resource[p.TofuType].(map[string]any)
		if !ok {
			bucket = map[string]any{}
			resource[p.TofuType] = bucket
		}
		bucket[p.TofuName] = attrs
	}
	return map[string]any{"resource": resource}
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
