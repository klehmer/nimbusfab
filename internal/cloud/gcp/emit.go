package gcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (a *Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	compType, _ := target.Spec["__type"].(string)
	switch compType {
	case "network":
		return a.emitNetwork(ctx, target, refs)
	case "compute":
		return a.emitCompute(ctx, target, refs)
	case "database":
		return a.emitDatabase(ctx, target, refs)
	case "storage":
		return a.emitStorage(ctx, target, refs)
	default:
		return nil, fmt.Errorf("gcp: unsupported component type %q", compType)
	}
}

// tofuIdent sanitizes a component name into a Tofu-safe local identifier
// (lowercase letters, digits, underscores; never starts with a digit).
var tofuIdentRe = regexp.MustCompile(`[^a-z0-9_]`)

func tofuIdent(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "_")
	s = tofuIdentRe.ReplaceAllString(s, "_")
	if s == "" {
		return "_"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}
	return s
}

// gcpResourceName converts a component name to a GCP-safe resource name:
// lowercase letters, digits, hyphens; must start with a letter; ≤63 chars.
var gcpResourceNameRe = regexp.MustCompile(`[^a-z0-9-]`)

func gcpResourceName(component string) string {
	s := strings.ToLower(component)
	s = strings.ReplaceAll(s, "_", "-")
	s = gcpResourceNameRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "x"
	}
	if !(s[0] >= 'a' && s[0] <= 'z') {
		s = "x" + s
	}
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// GCP-specific error sentinels.
var (
	ErrAdapterGCPMariaDBUnsupported = errors.New("Cloud SQL does not offer MariaDB; use postgres or mysql")
	ErrAdapterGCPUnsupportedEngine  = errors.New("unsupported database engine for GCP")
)
