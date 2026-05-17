// Package auth implements local-password auth, signed cookie sessions,
// and argon2id-hashed Personal Access Tokens. OIDC SSO is deferred to
// Auth Phase 2 (post-v1) and will layer on the same session foundation.
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// PasswordCost is the bcrypt cost factor. 12 is a reasonable 2026 default
// (~250ms on modern hardware); higher hurts login latency.
const PasswordCost = 12

// HashPassword hashes plaintext with bcrypt. Empty input → error.
func HashPassword(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, errors.New("auth: password cannot be empty")
	}
	return bcrypt.GenerateFromPassword([]byte(plaintext), PasswordCost)
}

// VerifyPassword returns true if plaintext matches the bcrypt hash.
// Empty hash or plaintext always returns false (defensive).
func VerifyPassword(hash []byte, plaintext string) bool {
	if len(hash) == 0 || plaintext == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, []byte(plaintext)) == nil
}
