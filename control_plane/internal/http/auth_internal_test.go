package http

import "testing"

func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"/catalog?x=1": "/catalog?x=1", "": "/", "https://evil.example": "/", "//evil.example": "/", "/\\evil.example": "/",
		"/auth/logout": "/", "javascript:alert(1)": "/", "/apps/../admin": "/", "/a%2f..%2fb": "/", "/apps/kms/": "/apps/kms/",
	}
	for in, want := range cases {
		if got := safeReturnTo(in); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(0.0001, 3)
	for i := 0; i < 3; i++ {
		if !l.allow("k") {
			t.Fatal("burst denied")
		}
	}
	if l.allow("k") || !l.allow("other") {
		t.Fatal("limiter not per key")
	}
}
