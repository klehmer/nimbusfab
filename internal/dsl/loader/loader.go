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
	_ = ctx
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

	// (Multi-file discovery, includes, stack values arrive in later tasks.)
	return proj, nil
}
