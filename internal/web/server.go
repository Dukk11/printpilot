// Package web serves the embedded single-page dashboard.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/Dukk11/printpilot/internal/printer"
	"github.com/Dukk11/printpilot/internal/store"
)

//go:embed static templates
var assets embed.FS

// staticFS is the /static subtree, served verbatim.
var staticFS fs.FS = mustSub(assets, "static")

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // build-time embedded path, cannot fail in practice
	}
	return sub
}

// Server renders the dashboard from the current store snapshot.
type Server struct {
	store     *store.Store
	demo      bool
	version   string
	startedAt time.Time
	tmpl      *template.Template
}

// New parses the embedded templates.
func New(st *store.Store, demo bool, version string) (*Server, error) {
	tmpl, err := template.New("web").Funcs(funcs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{store: st, demo: demo, version: version, startedAt: time.Now(), tmpl: tmpl}, nil
}

// Handler wires all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/", s.static)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/printers", s.apiPrinters)
	mux.HandleFunc("GET /fragment", s.fragment)
	mux.HandleFunc("GET /{$}", s.index)
	return mux
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))).ServeHTTP(w, r)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uptime_s": int(time.Since(s.startedAt).Seconds())})
}

func (s *Server) apiPrinters(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"printers": s.store.Snapshot(), "demo": s.demo})
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	s.render(w, "index.html")
} // fragment is the polled grid partial (the only part the client replaces).
func (s *Server) fragment(w http.ResponseWriter, _ *http.Request) {
	s.render(w, "cards")
}

func (s *Server) render(w http.ResponseWriter, name string) {
	data := map[string]any{
		"Printers": s.store.Snapshot(),
		"Demo":     s.demo,
		"Version":  s.version,
		"Online":   countState(s.store.Snapshot(), printer.StateOffline, false),
		"Printing": countState(s.store.Snapshot(), printer.StateRunning, true),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func countState(sts []printer.Status, st printer.State, equal bool) int {
	n := 0
	for _, s := range sts {
		if (s.State == st) == equal {
			n++
		}
	}
	return n
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// funcs exposes formatting helpers to the templates.
func funcs() template.FuncMap {
	return template.FuncMap{
		"pct": func(v float64) string {
			return strconv.Itoa(int(v + 0.5))
		},
		"remaining": func(min int) string {
			if min <= 0 {
				return ""
			}
			if min < 60 {
				return fmt.Sprintf("%d min", min)
			}
			return fmt.Sprintf("%dh %02dm", min/60, min%60)
		},
		"cam": func(st printer.Status) string {
			if st.WebcamURL == "" {
				return ""
			}
			sep := "?"
			for i := 0; i < len(st.WebcamURL); i++ {
				if st.WebcamURL[i] == '?' {
					sep = "&"
					break
				}
			}
			return st.WebcamURL + sep + "_pp=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		},
	}
}
