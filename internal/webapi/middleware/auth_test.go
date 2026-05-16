package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/webapi/middleware"
)

// next is a trivial handler used to detect "did the middleware let me through?"
var next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
})

func TestBearerToken_EmptyTokenIsNoop(t *testing.T) {
	h := middleware.BearerToken("")(next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Errorf("empty token should pass through; got status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestBearerToken_MissingHeaderReturns401(t *testing.T) {
	h := middleware.BearerToken("secret")(next)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ErrUnauthorized") {
		t.Errorf("body missing ErrUnauthorized: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestBearerToken_WrongTokenReturns401(t *testing.T) {
	h := middleware.BearerToken("secret")(next)
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerToken_CorrectTokenPasses(t *testing.T) {
	h := middleware.BearerToken("secret")(next)
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Errorf("correct token should pass; got status=%d body=%q", rec.Code, rec.Body.String())
	}
}
