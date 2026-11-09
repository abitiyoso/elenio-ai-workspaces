package http

import (
	"context"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func contextWith(ctx context.Context, pc proxyContext) context.Context {
	return context.WithValue(ctx, proxyCtxKey{}, pc)
}

// handleStatic serves the built React shell. Hashed assets are immutable; every other path gets
// index.html (client-side routing) with no-store so a deploy is picked up immediately.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}
	clean := path.Clean("/" + r.URL.Path)
	if strings.HasPrefix(clean, "/assets/") {
		full := filepath.Join(s.cfg.WebDir, filepath.FromSlash(clean))
		if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.ServeFile(w, r, full)
			return
		}
		http.NotFound(w, r)
		return
	}
	if clean != "/" && strings.Count(clean, "/") == 1 && strings.Contains(clean, ".") { // favicon.svg etc.
		full := filepath.Join(s.cfg.WebDir, filepath.FromSlash(clean))
		if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			http.ServeFile(w, r, full)
			return
		}
	}
	index := filepath.Join(s.cfg.WebDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		writePage(w, r, http.StatusServiceUnavailable, page{Title: "Web shell not built", Lines: []string{"Run npm run build in web/."}, Code: "WEB_NOT_BUILT"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, index)
}
