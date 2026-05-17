package middleware

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// AuditLog wraps a handler to append one inventory.AuditEntry per
// successful (status < 400) request. The verb argument identifies the
// operation; the target is read from the {id} path param when present.
//
// Failures are best-effort: a repo error logs nowhere (the engine logger
// isn't threaded into the middleware) but doesn't affect the response.
// Wrap mutating endpoints only; read endpoints would 10× the table.
//
// Pattern: audit("deployment.apply")(handler).
func AuditLog(repo inventory.Repo) func(verb string) func(http.Handler) http.Handler {
	return func(verb string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := &statusRecorder{ResponseWriter: w, status: 200}
				next.ServeHTTP(rec, r)
				if rec.status >= 400 {
					return
				}
				u := UserFromContext(r.Context())
				if u == nil {
					return // anonymous; shouldn't happen on auth'd routes but be defensive
				}
				target := r.PathValue("id")
				// Fire-and-forget: never block the response on a slow audit write.
				go func() {
					_ = repo.AuditLog().Append(context.Background(), inventory.AuditEntry{
						OrgID:       u.OrgID,
						ActorUserID: u.ID,
						Verb:        verb,
						Target:      target,
						Timestamp:   time.Now().UTC(),
					})
				}()
			})
		}
	}
}

// statusRecorder captures the status code so the audit middleware can
// skip writing on error responses.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(p)
}

// Flush proxies to the underlying ResponseWriter when it supports
// http.Flusher (needed for SSE-adjacent code paths even though the audit
// middleware doesn't wrap SSE itself).
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
