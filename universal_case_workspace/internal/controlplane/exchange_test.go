package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elenio/case-workspace/internal/auth"
)

func TestExchange(t *testing.T) {
	calls, status := 0, 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		u, p, _ := r.BasicAuth()
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if u != "case-workspace" || p != "secret-secret-secret" || in["audience"] != "flowcase" || in["subject_token"] != "incoming" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "out", "expires_in": 60})
	}))
	defer srv.Close()
	now := time.Now()
	c, _ := New(Config{BaseURL: srv.URL, ClientID: "case-workspace", ClientSecret: "secret-secret-secret", Now: func() time.Time { return now }})
	id := auth.Identity{UserID: "u", SessionRef: "s1", Token: "incoming"}
	for range 2 {
		if tok, err := c.Token(context.Background(), id, "flowcase"); err != nil || tok != "out" {
			t.Fatal(tok, err)
		}
	}
	if calls != 1 {
		t.Fatalf("not cached: %d calls", calls)
	}
	now = now.Add(31 * time.Second) // within 30s of expiry -> refresh
	_, _ = c.Token(context.Background(), id, "flowcase")
	if calls != 2 {
		t.Fatal("not refreshed before expiry")
	}
	id2 := id
	id2.SessionRef = "s2"
	status = 401
	if _, err := c.Token(context.Background(), id2, "flowcase"); !errors.Is(err, ErrSessionInactive) {
		t.Fatalf("401 -> %v", err)
	}
	status = 502
	if _, err := c.Token(context.Background(), id2, "flowcase"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("502 -> %v", err)
	}
}
