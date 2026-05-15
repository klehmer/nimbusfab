//go:build tools

// Package tools tracks build-time tool and Phase 1 runtime dependencies
// via go.mod so that `go mod tidy` does not prune them before downstream
// tasks import them from real source files.
//
// `go install` the tool entries below to build the schemagen tool.
// The runtime entries (yaml.v3, jsonschema/v5, cobra) are placeholders
// until tasks 5, 11, and 12 introduce real imports; at that point they
// should be removed from this file.
package tools

import (
	// Build-time tools.
	_ "github.com/invopop/jsonschema"

	// Phase 1 runtime dependencies, pinned here so `go mod tidy` keeps
	// them in go.mod until the corresponding tasks land their imports.
	_ "github.com/santhosh-tekuri/jsonschema/v5"
	_ "github.com/spf13/cobra"
	_ "gopkg.in/yaml.v3"
)
