package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EnvBackend resolves refs from process environment variables. Convention:
// ref "aws-dev" maps to env var "NIMBUSFAB_SECRET_AWS_DEV". The env var's
// value MUST be a JSON object; the parsed map is returned. Missing env vars
// produce (nil, nil) so the backend can be chained.
type EnvBackend struct{}

// Kind returns "env".
func (*EnvBackend) Kind() string { return "env" }

// Resolve looks up NIMBUSFAB_SECRET_<UPPER_REF> and JSON-decodes it.
func (*EnvBackend) Resolve(ctx context.Context, ref string) (map[string]any, error) {
	_ = ctx
	if ref == "" {
		return nil, nil
	}
	key := "NIMBUSFAB_SECRET_" + envify(ref)
	raw := os.Getenv(key)
	if raw == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("secrets/env: %s contains invalid JSON: %w", key, err)
	}
	return out, nil
}

// envify uppercases the ref and converts hyphens to underscores so a YAML
// credentialRef like "aws-dev" maps to env var NIMBUSFAB_SECRET_AWS_DEV.
func envify(ref string) string {
	return strings.ToUpper(strings.ReplaceAll(ref, "-", "_"))
}
