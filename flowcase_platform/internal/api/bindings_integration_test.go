package api_test

import (
	"strconv"
	"testing"
)

func TestBindingEvidenceHTTPUsesExistingWorkflowAuthorization(t *testing.T) {
	c, member := newServer(t)
	if w, b := c.do("POST", "/api/v1/workflows", map[string]any{"key": "binding-http", "template": "internal-poc-request"}, map[string]string{"Idempotency-Key": "binding-http-create"}); w.Code != 201 {
		t.Fatalf("create: %d %v", w.Code, b)
	}
	if w, _ := member.do("GET", "/api/v1/workflows/binding-http/versions/1/dependencies", nil, nil); w.Code != 409 {
		t.Fatal("draft binding endpoint", w.Code)
	}
	// Original workflow publication remains the only way to obtain source evidence.
	w, validated := c.do("POST", "/api/v1/workflows/binding-http/versions/1/validate", nil, nil)
	if w.Code != 200 {
		t.Fatalf("validate: %d %v", w.Code, validated)
	}
	v := validated["version"].(map[string]any)["version_num"].(float64)
	if w, b := c.do("POST", "/api/v1/workflows/binding-http/versions/1/publish", nil, map[string]string{"If-Match": strconv.Quote(strconv.Itoa(int(v)))}); w.Code != 200 {
		t.Fatalf("publish: %d %v", w.Code, b)
	}
	w, body := member.do("GET", "/api/v1/workflows/binding-http/versions/1/dependencies", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || body["schema"] != "elenio.workflow-bindings.v1" || body["workflow_key"] != "binding-http" {
		t.Fatalf("binding endpoint: %d %s", w.Code, w.Body)
	}
	if w.Header().Get("ETag") != strconv.Quote(body["content_hash"].(string)) {
		t.Fatal("binding digest missing")
	}
}
