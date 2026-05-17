package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session is the payload carried in the signed cookie. Stateless: no
// server-side session table; the signed cookie is self-contained.
type Session struct {
	UserID    string    `json:"u"`
	OrgID     string    `json:"o"`
	Email     string    `json:"e"`
	ExpiresAt time.Time `json:"x"`
}

// Valid returns nil if the session has not expired.
func (s *Session) Valid(now time.Time) error {
	if s == nil {
		return errors.New("auth: nil session")
	}
	if !now.Before(s.ExpiresAt) {
		return errors.New("auth: session expired")
	}
	return nil
}

// SignSession encodes the session as JSON + base64 and appends an HMAC-SHA256
// signature over the encoded payload. Format: "<b64-payload>.<b64-sig>".
func SignSession(key []byte, s Session) (string, error) {
	if len(key) < 16 {
		return "", errors.New("auth: signing key too short (need ≥16 bytes)")
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("auth: marshal session: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

// VerifySession returns the decoded Session iff the cookie's signature is
// valid AND the session has not expired. Constant-time signature comparison.
func VerifySession(key []byte, cookie string) (*Session, error) {
	if len(key) < 16 {
		return nil, errors.New("auth: signing key too short")
	}
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("auth: malformed session cookie")
	}
	encoded, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return nil, errors.New("auth: invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("auth: decode session: %w", err)
	}
	var s Session
	if err := json.Unmarshal(payload, &s); err != nil {
		return nil, fmt.Errorf("auth: unmarshal session: %w", err)
	}
	if err := s.Valid(time.Now()); err != nil {
		return nil, err
	}
	return &s, nil
}
