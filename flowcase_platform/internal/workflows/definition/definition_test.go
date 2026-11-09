package definition

import (
	"encoding/json"
	"testing"
)

func mustDef(t *testing.T, raw string) Definition {
	t.Helper()
	var d Definition
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestHashIsCanonicalAndIgnoresStatus(t *testing.T) {
	a := mustDef(t, `{"schema_version":"1.0","key":"k","name":"Name","version":1,"status":"draft",
		"metadata":{"b":1,"a":{"y":true,"x":null}},"nodes":[{"id":"start","type":"start","name":"S","next":["end"]},{"id":"end","type":"end","name":"E","next":[]}]}`)
	b := mustDef(t, `{"nodes":[{"next":["end"],"name":"S","type":"start","id":"start"},{"name":"E","id":"end","type":"end","next":[]}],
		"metadata":{"a":{"x":null,"y":true},"b":1},"version":1,"name":"Name","key":"k","schema_version":"1.0","status":"published"}`)
	ha, err := Hash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := Hash(b)
	if ha != hb || len(ha) != 64 {
		t.Fatalf("hash must be canonical and status-independent: %s vs %s", ha, hb)
	}
	b.Nodes[0].Name = "Changed"
	if hc, _ := Hash(b); hc == ha {
		t.Fatal("content change must change the hash")
	}
}

func parse(t *testing.T, raw string) Expr {
	t.Helper()
	v, err := DecodeRaw([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	e, diags := ParseExpr(v, "/e", "n")
	if len(diags) > 0 {
		t.Fatalf("unexpected diagnostics %+v", diags)
	}
	return e
}

func TestExpressionEvaluation(t *testing.T) {
	vars := map[string]any{"estimated_cost": json.Number("1500"), "region": "id", "flags": map[string]any{"urgent": true}, "nothing": nil}
	cases := map[string]bool{
		`{"operator":"gt","field":"estimated_cost","value":0}`:     true,
		`{"operator":"lte","field":"estimated_cost","value":1000}`: false,
		`{"operator":"gt","field":"missing","value":0}`:            false,
		`{"operator":"eq","field":"region","value":"id"}`:          true,
		`{"operator":"ne","field":"missing","value":"x"}`:          true,
		`{"operator":"in","field":"region","value":["sg","id"]}`:   true,
		`{"operator":"not_in","field":"region","value":["sg"]}`:    true,
		`{"operator":"exists","field":"flags.urgent"}`:             true,
		`{"operator":"exists","field":"nothing"}`:                  false,
		`{"operator":"eq","field":"flags.urgent","value":true}`:    true,
		`{"operator":"and","conditions":[{"operator":"exists","field":"region"},{"operator":"gt","field":"estimated_cost","value":1000}]}`: true,
		`{"operator":"or","conditions":[{"operator":"exists","field":"missing"},{"operator":"eq","field":"region","value":"sg"}]}`:         false,
		`{"operator":"not","condition":{"operator":"exists","field":"missing"}}`:                                                           true,
	}
	for raw, want := range cases {
		got, err := parse(t, raw).Eval(vars)
		if err != nil || got != want {
			t.Errorf("%s: got %v (%v), want %v", raw, got, err, want)
		}
	}
	if _, err := parse(t, `{"operator":"gt","field":"region","value":1}`).Eval(vars); err == nil {
		t.Error("numeric comparison on a string variable must be an error, not a silent route")
	}
}

func TestExpressionFields(t *testing.T) {
	e := parse(t, `{"operator":"and","conditions":[{"operator":"exists","field":"a"},{"operator":"not","condition":{"operator":"eq","field":"b.c","value":1}}]}`)
	got := e.Fields()
	if len(got) != 2 || got[0] != "a" || got[1] != "b.c" {
		t.Fatalf("unexpected fields %v", got)
	}
}

func TestValidateData(t *testing.T) {
	v, _ := DecodeRaw([]byte(`{"type":"object","required":["requester_id","purpose"],"additionalProperties":false,
		"properties":{"requester_id":{"type":"string","minLength":2},"purpose":{"type":"string","maxLength":10},
		"estimated_cost":{"type":"number","minimum":0},"deadline":{"type":"string","format":"date-time"},
		"tier":{"type":"string","enum":["gold","silver"]},"tags":{"type":"array","items":{"type":"string"}}}}`))
	schema := v.(map[string]any)
	ok := map[string]any{"requester_id": "u-1", "purpose": "pilot", "estimated_cost": json.Number("10.5"), "deadline": "2026-11-01T10:00:00Z", "tier": "gold", "tags": []any{"a"}}
	if d := ValidateData(schema, ok, ""); len(d) != 0 {
		t.Fatalf("valid data rejected: %+v", d)
	}
	bad := map[string]any{"purpose": "far too long purpose", "estimated_cost": json.Number("-1"), "deadline": "tomorrow", "tier": "bronze", "tags": []any{1}, "extra": true}
	codes := map[string]string{}
	for _, d := range ValidateData(schema, bad, "/variables") {
		codes[d.Path] = d.Code
	}
	want := map[string]string{"/variables/requester_id": "required", "/variables/purpose": "too_long", "/variables/estimated_cost": "too_small",
		"/variables/deadline": "invalid_format", "/variables/tier": "not_allowed", "/variables/tags/0": "type_mismatch", "/variables/extra": "unknown_property"}
	for path, code := range want {
		if codes[path] != code {
			t.Errorf("%s: want %s, got %q (all: %v)", path, code, codes[path], codes)
		}
	}
}
