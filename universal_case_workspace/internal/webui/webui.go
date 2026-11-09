// Package webui serves the built SPA (web/dist) from the directory named by UCW_WEB_DIR. Unknown client
// routes fall back to index.html; /api/ is never answered with the SPA.
package webui

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const spaCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; " +
	"connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'self'"

type Handler struct {
	fsys  fs.FS
	index []byte
	mod   time.Time
}

// New opens dir with an os.Root (no escapes through symlinks or ..) and requires index.html.
func New(dir string) (*Handler, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	fsys := root.FS()
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, errors.New("webui: index.html not found in UCW_WEB_DIR")
	}
	st, _ := fs.Stat(fsys, "index.html")
	h := &Handler{fsys: fsys, index: index}
	if st != nil {
		h.mod = st.ModTime()
	}
	return h, nil
}

func headers(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", spaCSP)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	headers(w)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "index.html" {
		h.serveIndex(w, r)
		return
	}
	if fs.ValidPath(name) {
		if f, err := h.fsys.Open(name); err == nil {
			defer f.Close()
			if st, err := f.Stat(); err == nil && !st.IsDir() {
				if rs, ok := f.(io.ReadSeeker); ok {
					if strings.HasPrefix(name, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						w.Header().Set("Cache-Control", "no-cache")
					}
					http.ServeContent(w, r, st.Name(), st.ModTime(), rs)
					return
				}
			}
		}
	}
	if strings.HasPrefix(name, "assets/") { // a missing hashed asset is a 404, not the app shell
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", h.mod, bytes.NewReader(h.index))
}
