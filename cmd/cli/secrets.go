package main

import "github.com/klehmer/nimbusfab/pkg/secrets"

// defaultSecretsBackend returns the Phase-1 default backend chain (env-var
// first, then file at ~/.nimbusfab/secrets/). Callers that want a different
// backend (Vault, KMS, etc.) construct engine.Config.SecretsBackend
// explicitly instead of calling this.
func defaultSecretsBackend() secrets.Backend {
	return secrets.DefaultBackend()
}
