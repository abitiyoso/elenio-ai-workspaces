// Opt-in development tooling; never part of control-plane-api or a production image.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elenio/control-plane/internal/localdev"
)

func main() {
	mode := flag.String("mode", "", "identity or gateway")
	secretFile := flag.String("client-secret-file", "", "local protected client secret file")
	seedFile := flag.String("seed-file", "deploy/dev-seed.json", "synthetic CP seed file")
	web := flag.String("web-dir", "", "mounted Flowcase web build")
	flag.Parse()
	if os.Getenv("ELENIO_LOCAL_E2E") != "1" || os.Getenv("APP_ENV") != "development" {
		fail(errors.New("local workspace tools require ELENIO_LOCAL_E2E=1 and APP_ENV=development"))
	}
	var handler http.Handler
	address := ""
	switch *mode {
	case "identity":
		raw, err := os.ReadFile(*secretFile)
		if err != nil {
			fail(errors.New("local client secret unavailable"))
		}
		var seed struct {
			Users []localdev.User `json:"users"`
		}
		data, err := os.ReadFile(*seedFile)
		if err != nil || json.Unmarshal(data, &seed) != nil {
			fail(errors.New("local synthetic seed unavailable"))
		}
		idp, err := localdev.NewIdentity(localdev.IdentityOptions{Issuer: "http://127.0.0.1:8210", Redirect: "http://127.0.0.1:8200/auth/callback", ClientID: "control-plane", ClientSecret: strings.TrimSpace(string(raw)), Users: seed.Users})
		if err != nil {
			fail(err)
		}
		handler = idp.Handler()
		address = "127.0.0.1:8210"
	case "gateway":
		var err error
		handler, err = gateway(*web)
		if err != nil {
			fail(err)
		}
		address = "127.0.0.1:5280"
	default:
		fail(errors.New("choose identity or gateway mode"))
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	if err := server.ListenAndServe(); err != nil {
		fail(errors.New("local workspace listener unavailable"))
	}
}
func gateway(web string) (http.Handler, error) {
	dir, err := filepath.Abs(web)
	if err != nil || web == "" {
		return nil, errors.New("built mounted web directory required")
	}
	for _, file := range []string{"index.html", "elenio.html"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			return nil, errors.New("build the CP-mounted Elenio frontend first")
		}
	}
	target, _ := url.Parse("http://127.0.0.1:8280")
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) { original(r); r.Header.Del("Cookie"); r.Header.Del("Proxy-Authorization") }
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "local workflow backend unavailable", 502)
	}
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:5280" {
			http.Error(w, "loopback gateway host required", 403)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/readyz" || r.URL.Path == "/healthz" {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}), nil
}
func fail(err error) { fmt.Fprintln(os.Stderr, err.Error()); os.Exit(1) }
