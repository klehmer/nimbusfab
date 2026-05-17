package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PATPrefixLen is the number of URL-safe characters used as the public,
// lookup-able prefix. Long enough to be effectively unique across an org's
// PATs, short enough that printed tokens stay readable.
const PATPrefixLen = 8

// PATSecretLen is the random secret-portion byte count (before base64).
const PATSecretLen = 24

// argon2 parameters — modest defaults that authenticate in <10ms on
// commodity hardware. Real production tuning lands in Polish Phase 1.
const (
	argonTime    = 1
	argonMemory  = 32 * 1024 // 32 MiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// GeneratePAT returns a token of the form "nfp_<prefix>_<secret>", the
// prefix in plaintext (for DB indexing), and the argon2id hash of the
// secret part (for DB storage).
func GeneratePAT() (token, prefix string, hash []byte, err error) {
	prefixBytes := make([]byte, 6) // 6 bytes → 8 base64url chars
	if _, err = rand.Read(prefixBytes); err != nil {
		return "", "", nil, err
	}
	prefix = base64.RawURLEncoding.EncodeToString(prefixBytes)

	secretBytes := make([]byte, PATSecretLen)
	if _, err = rand.Read(secretBytes); err != nil {
		return "", "", nil, err
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)

	hash, err = hashSecret(secret)
	if err != nil {
		return "", "", nil, err
	}
	// Separator is "." — not in base64url alphabet so the split is
	// unambiguous (vs "_" which IS in base64url and caused random parse
	// failures when the prefix happened to contain one).
	token = "nfp_" + prefix + "." + secret
	return token, prefix, hash, nil
}

// ParsePAT splits "nfp_<prefix>.<secret>" into its parts. Returns
// (prefix, secret, ok). ok=false if the token doesn't match the expected
// shape.
func ParsePAT(token string) (prefix, secret string, ok bool) {
	if !strings.HasPrefix(token, "nfp_") {
		return "", "", false
	}
	rest := token[len("nfp_"):]
	parts := strings.SplitN(rest, ".", 2)
	if len(parts) != 2 || len(parts[0]) != PATPrefixLen || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// VerifyPAT returns true if the secret matches the stored argon2 hash.
// Constant-time comparison.
func VerifyPAT(secret string, storedHash []byte) bool {
	// Stored hash format: salt || key. salt is the first argonSaltLen bytes.
	if len(storedHash) != argonSaltLen+argonKeyLen {
		return false
	}
	salt := storedHash[:argonSaltLen]
	want := storedHash[argonSaltLen:]
	got := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(want, got) == 1
}

// hashSecret produces salt || key for storage.
func hashSecret(secret string) ([]byte, error) {
	if secret == "" {
		return nil, errors.New("auth: empty PAT secret")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return append(salt, key...), nil
}
