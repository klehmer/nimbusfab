// Package loader walks a project directory and returns an unvalidated
// *ir.Project. The Phase-1 implementation supports the canonical layout
// (project.yaml + components/ + compositions/) and the single-file
// fallback. Validation is the next subsystem; this package is only
// concerned with file discovery and YAML parsing.
package loader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/klehmer/nimbusfab/internal/dsl/yamlnode"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Loader is the public entry point. Construct with New().
type Loader interface {
	Load(ctx context.Context, root string) (*ir.Project, error)
}

// New returns the default Loader implementation.
func New() Loader { return &fsLoader{} }

type fsLoader struct{}

func (l *fsLoader) Load(ctx context.Context, root string) (*ir.Project, error) {
	projectPath := filepath.Join(root, "project.yaml")
	body, err := os.ReadFile(projectPath)
	if err != nil {
		return nil, fmt.Errorf("read project.yaml: %w", err)
	}
	doc, err := yamlnode.Decode(projectPath, body)
	if err != nil {
		return nil, err
	}
	proj := &ir.Project{}
	if err := doc.Raw.Decode(proj); err != nil {
		return nil, &yamlnode.Error{
			Source: ir.Source{File: projectPath, Line: doc.Raw.Line},
			Err:    fmt.Errorf("decode project: %w", err),
		}
	}

	if err := l.loadComponentsDir(root, proj); err != nil {
		return nil, err
	}
	if err := l.loadCompositionsDir(root, proj); err != nil {
		return nil, err
	}
	if err := assertUniqueComponents(proj); err != nil {
		return nil, err
	}
	if err := assertUniqueCompositions(proj); err != nil {
		return nil, err
	}

	_ = ctx
	return proj, nil
}

func (l *fsLoader) loadComponentsDir(root string, proj *ir.Project) error {
	dir := filepath.Join(root, "components")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read components/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		// Components files contain one Component each (multi-doc support in a later task).
		doc, err := yamlnode.Decode(path, body)
		if err != nil {
			return err
		}
		var comp ir.Component
		if err := doc.Raw.Decode(&comp); err != nil {
			return &yamlnode.Error{
				Source: ir.Source{File: path, Line: doc.Raw.Line},
				Err:    fmt.Errorf("decode component: %w", err),
			}
		}
		proj.Components = append(proj.Components, comp)
	}
	return nil
}

func (l *fsLoader) loadCompositionsDir(root string, proj *ir.Project) error {
	dir := filepath.Join(root, "compositions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read compositions/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		doc, err := yamlnode.Decode(path, body)
		if err != nil {
			return err
		}
		var comp ir.Composition
		if err := doc.Raw.Decode(&comp); err != nil {
			return &yamlnode.Error{
				Source: ir.Source{File: path, Line: doc.Raw.Line},
				Err:    fmt.Errorf("decode composition: %w", err),
			}
		}
		proj.Comps = append(proj.Comps, comp)
	}
	return nil
}

func assertUniqueComponents(proj *ir.Project) error {
	seen := map[string]bool{}
	for _, c := range proj.Components {
		if seen[c.Name] {
			return fmt.Errorf("duplicate component %q (a component with this name appears more than once across project.yaml and components/)", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

func assertUniqueCompositions(proj *ir.Project) error {
	seen := map[string]bool{}
	for _, c := range proj.Comps {
		if seen[c.Kind] {
			return fmt.Errorf("duplicate composition kind %q (a composition with this kind appears more than once across project.yaml and compositions/)", c.Kind)
		}
		seen[c.Kind] = true
	}
	return nil
}
