package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/auth"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !auth.VerifyPassword(hash, "hunter2") {
		t.Error("VerifyPassword: correct password rejected")
	}
	if auth.VerifyPassword(hash, "wrong") {
		t.Error("VerifyPassword: wrong password accepted")
	}
	if auth.VerifyPassword(nil, "hunter2") {
		t.Error("VerifyPassword: empty hash accepted")
	}
	if auth.VerifyPassword(hash, "") {
		t.Error("VerifyPassword: empty plaintext accepted")
	}
}

func TestHashPassword_EmptyRejected(t *testing.T) {
	if _, err := auth.HashPassword(""); err == nil {
		t.Error("HashPassword(\"\"): want error")
	}
}

func TestSignVerifySession_RoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	s := auth.Session{
		UserID:    "u1",
		OrgID:     "o1",
		Email:     "a@x",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	cookie, err := auth.SignSession(key, s)
	if err != nil {
		t.Fatalf("SignSession: %v", err)
	}
	got, err := auth.VerifySession(key, cookie)
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if got.UserID != "u1" || got.OrgID != "o1" || got.Email != "a@x" {
		t.Errorf("session fields mangled: %+v", got)
	}
}

func TestSession_TamperedSigRejected(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	cookie, _ := auth.SignSession(key, auth.Session{UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	// Flip a character in the signature half.
	tampered := cookie[:len(cookie)-1] + "X"
	if _, err := auth.VerifySession(key, tampered); err == nil {
		t.Error("tampered cookie should fail verification")
	}
}

func TestSession_WrongKeyRejected(t *testing.T) {
	k1 := []byte("0123456789abcdef0123456789abcdef")
	k2 := []byte("ffffffffffffffffffffffffffffffff")
	cookie, _ := auth.SignSession(k1, auth.Session{UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	if _, err := auth.VerifySession(k2, cookie); err == nil {
		t.Error("cookie signed with k1 should not verify with k2")
	}
}

func TestSession_ExpiredRejected(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	cookie, _ := auth.SignSession(key, auth.Session{UserID: "u1", ExpiresAt: time.Now().Add(-time.Hour)})
	if _, err := auth.VerifySession(key, cookie); err == nil {
		t.Error("expired cookie should fail")
	}
}

func TestSession_ShortKeyRejected(t *testing.T) {
	if _, err := auth.SignSession([]byte("short"), auth.Session{}); err == nil {
		t.Error("short key should be rejected at sign time")
	}
}

func TestPAT_GenerateAndVerify(t *testing.T) {
	token, prefix, hash, err := auth.GeneratePAT()
	if err != nil {
		t.Fatalf("GeneratePAT: %v", err)
	}
	if !strings.HasPrefix(token, "nfp_") {
		t.Errorf("token should start nfp_: %s", token)
	}
	if len(prefix) != auth.PATPrefixLen {
		t.Errorf("prefix len = %d, want %d", len(prefix), auth.PATPrefixLen)
	}
	gotPrefix, secret, ok := auth.ParsePAT(token)
	if !ok {
		t.Fatalf("ParsePAT: not ok")
	}
	if gotPrefix != prefix {
		t.Errorf("parsed prefix mismatch: %q vs %q", gotPrefix, prefix)
	}
	if !auth.VerifyPAT(secret, hash) {
		t.Error("VerifyPAT: correct secret rejected")
	}
	if auth.VerifyPAT("wrong", hash) {
		t.Error("VerifyPAT: wrong secret accepted")
	}
}

func TestPAT_ParseRejectsBadShapes(t *testing.T) {
	for _, bad := range []string{"", "nope_x.y", "nfp_short.y", "nfp_12345678", "nfp_12345678."} {
		if _, _, ok := auth.ParsePAT(bad); ok {
			t.Errorf("ParsePAT(%q) should fail", bad)
		}
	}
}

// TestPAT_GenerateMany sanity-checks that 100 generations all round-trip
// — defends against the underscore-in-base64url separator bug we hit
// during Polish Phase 1.
func TestPAT_GenerateMany(t *testing.T) {
	for i := 0; i < 100; i++ {
		token, prefix, hash, err := auth.GeneratePAT()
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		gotPrefix, secret, ok := auth.ParsePAT(token)
		if !ok || gotPrefix != prefix {
			t.Fatalf("iter %d: ParsePAT failed for token=%q prefix=%q", i, token, prefix)
		}
		if !auth.VerifyPAT(secret, hash) {
			t.Fatalf("iter %d: VerifyPAT failed", i)
		}
	}
}
