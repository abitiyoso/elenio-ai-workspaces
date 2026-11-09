package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPAServing(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=root></div>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644)
	h, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w
	}
	for _, p := range []string{"/", "/cases/123", "/packs/sim.tender/versions/1.1.0", "/cases/new?pack=x"} {
		w := get(p)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() == "" {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	if w := get("/assets/app-abc123.js"); w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("asset: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := get("/assets/missing.js"); w.Code != 404 {
		t.Errorf("missing asset: %d", w.Code)
	}
	if w := get("/api/v1/workspace/cases"); w.Code != 404 {
		t.Errorf("api served by SPA: %d", w.Code)
	}
	if w := get("/../../etc/passwd"); w.Code != 200 || w.Body.String() != "<!doctype html><div id=root></div>" {
		t.Errorf("traversal: %d", w.Code)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/cases", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("post: %d", w.Code)
	}
}
