package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayRootAssetsAndDirectoryBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<!doctype html>workspace", "elenio.html": "<!doctype html>elenio", "assets/app.js": "export const ready=true;"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := gateway(dir, "127.0.0.1:5280", "http://127.0.0.1:8280", []string{"index.html", "elenio.html"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		method, path string
		status       int
		body         string
	}{{"GET", "/", 200, "<!doctype html>workspace"}, {"GET", "/elenio.html", 200, "<!doctype html>elenio"}, {"GET", "/assets/app.js", 200, "export const ready=true;"}, {"GET", "/assets/", 404, "404 page not found"}, {"POST", "/elenio.html", 405, "method not allowed"}} {
		req := httptest.NewRequest(item.method, "http://127.0.0.1:5280"+item.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != item.status || !strings.Contains(rec.Body.String(), item.body) {
			t.Errorf("%s %s: %d %q", item.method, item.path, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "http://foreign.invalid/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("foreign gateway host accepted")
	}
}

func TestGatewayKeepsBearerButDropsBrowserCookiesAndProxyAuthorization(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session" || r.Header.Get("Authorization") != "Bearer synthetic-audience-token" || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("gateway forwarded incorrect authentication boundary")
		}
		w.WriteHeader(202)
		_, _ = io.WriteString(w, "verified")
	}))
	defer backend.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0600); err != nil {
		t.Fatal(err)
	}
	handler, err := gateway(dir, "127.0.0.1:5380", backend.URL, []string{"index.html"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:5380/api/v1/session", nil)
	req.Header.Set("Authorization", "Bearer synthetic-audience-token")
	req.Header.Set("Cookie", "synthetic=not-forwarded")
	req.Header.Set("Proxy-Authorization", "Basic not-forwarded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 202 || rec.Body.String() != "verified" {
		t.Fatal("backend response not preserved")
	}
}
