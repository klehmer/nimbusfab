// Package ui implements server-rendered HTML pages for the nimbusfab web
// app. UI Phase 1 ships read-only views over the inventory repo; mutating
// actions land in HTTP Phase 2 / UI Phase 2.
package ui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed assets
var assetsFS embed.FS

// AssetsFS returns the embedded /assets/* sub-filesystem suitable for
// http.FileServerFS.
func AssetsFS() (fs.FS, error) {
	return fs.Sub(assetsFS, "assets")
}

// Renderer holds parsed templates and the engine deps page handlers need.
type Renderer struct {
	Repo  inventory.Repo
	OrgID string

	tmpl *template.Template
}

// NewRenderer parses every template under templates/. Each page template
// composes the layout via {{template "layout" .}} and defines its own
// {{define "title"}} and {{define "content"}} blocks.
func NewRenderer(repo inventory.Repo, orgID string) (*Renderer, error) {
	t, err := template.New("").Funcs(funcMap()).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("ui: parse templates: %w", err)
	}
	return &Renderer{Repo: repo, OrgID: orgID, tmpl: t}, nil
}

// render writes one page using the named page template.
func (r *Renderer) render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (r *Renderer) renderError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	r.render(w, "error.html", map[string]any{
		"Status":  status,
		"Message": msg,
	})
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"humanStatus": humanStatus,
		"statusBadge": statusBadge,
		"shortID": func(id string) string {
			if len(id) > 14 {
				return id[:14] + "…"
			}
			return id
		},
		"defaultStr": func(s, def string) string {
			if s == "" {
				return def
			}
			return s
		},
	}
}

func humanStatus(s string) string {
	switch s {
	case "succeeded":
		return "succeeded"
	case "failed":
		return "failed"
	case "partial_failure":
		return "partial failure"
	case "planned":
		return "planned"
	case "running":
		return "running"
	}
	return s
}

// statusBadge returns the CSS class for a status badge.
func statusBadge(s string) string {
	switch s {
	case "succeeded":
		return "ok"
	case "failed":
		return "fail"
	case "partial_failure":
		return "warn"
	}
	return ""
}
