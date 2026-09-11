package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dukk11/printpilot/internal/printer"
	"github.com/Dukk11/printpilot/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st := store.New([]string{"x1c"})
	_, _ = st.Update(printer.Status{ID: "x1c", Type: printer.TypeBambu, Name: "X1C", State: printer.StateRunning, Progress: 40, JobName: "benchy.gcode"})
	srv, err := New(st, true, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func TestIndexContainsCards(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("index status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"PrintPilot", "X1C", "benchy.gcode", "app.css"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

// Regression: the fragment must render the cards template, not the bare
// define-only file (which silently yields an empty 200 response).
func TestFragmentRendersCards(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/fragment", nil))
	if rec.Code != 200 {
		t.Fatalf("fragment status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "benchy.gcode") {
		t.Fatalf("fragment missing printer card, got: %q", rec.Body.String())
	}
}

func TestAPIPrintersJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/printers", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"benchy.gcode"`) {
		t.Fatalf("api = %d %s", rec.Code, rec.Body.String())
	}
}

func TestStaticAssetsServed(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/static/app.css", "/static/app.js", "/static/logo.svg"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
	}
}
