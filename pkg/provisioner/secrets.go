package provisioner

import (
	"context"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/secrets"
)

// resolveEnvFor turns a credentialRef into a string-keyed env var map by
// asking the configured secrets backend. Nil backend or empty ref → empty
// map, no error (caller proceeds with whatever the process env contains —
// preserves the pre-Phase-1 behavior).
//
// A non-nil backend that can't resolve a non-empty ref returns
// ErrSecretsRefUnresolved so the orchestrator can fail the target fast,
// before any tofu invocation. Non-string payload values also return an
// error since they can't be turned into env vars.
func resolveEnvFor(ctx context.Context, backend secrets.Backend, ref string) (map[string]string, error) {
	if backend == nil || ref == "" {
		return map[string]string{}, nil
	}
	payload, err := backend.Resolve(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("ErrSecretsRefUnresolved: resolving %q: %w", ref, err)
	}
	if payload == nil {
		return nil, fmt.Errorf("ErrSecretsRefUnresolved: no backend resolved credentialRef %q", ref)
	}
	env := make(map[string]string, len(payload))
	for k, v := range payload {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("secrets payload for %q: key %q has non-string value (%T)", ref, k, v)
		}
		env[k] = s
	}
	return env, nil
}
