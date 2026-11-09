package packs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const packsDir = "../../domain-packs"

func TestBundledPacksValid(t *testing.T) {
	reg, err := Load(context.Background(), packsDir, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, p := range reg.All() {
		if !p.Valid() {
			t.Errorf("%s %s (%s) invalid: %+v", p.Key, p.Version, p.SourceFile, p.Errors())
		}
		keys[p.Key] = true
	}
	for _, k := range []string{"sim.tender", "legal.contract-review", "lab.poc-assessment"} {
		if !keys[k] {
			t.Errorf("pack %s not loaded", k)
		}
	}
	sim := reg.LatestPublished("sim.tender")
	if sim == nil || sim.Version != "1.3.0" || sim.Manifest.Intake == nil || sim.Manifest.Intake.Documents == nil {
		t.Fatalf("latest sim pack = %+v", sim)
	}
	if reg.LatestForWorkflow("sim-tender").Version != "1.3.0" {
		t.Fatal("workflow binding lookup")
	}
	if at := sim.Manifest.ArtifactTypeFor("requirement-register"); at == nil || at.Source.RowsPath != "requirements" {
		t.Fatalf("source mapping = %+v", at)
	}
}

func simRaw(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packsDir, "sim-tender.v1.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func codes(p *Pack) string {
	var out []string
	for _, e := range p.Errors() {
		out = append(out, e.Code+"@"+e.Path)
	}
	return strings.Join(out, " ")
}

func TestHostileManifestsRejected(t *testing.T) {
	cases := []struct {
		name string
		mut  func(m map[string]any)
		want string
	}{
		{"external URL in icon", func(m map[string]any) {
			m["navigation"].([]any)[0].(map[string]any)["icon"] = "https://evil.example/x.js"
		}, "url_not_allowed"},
		{"script label", func(m map[string]any) {
			m["navigation"].([]any)[1].(map[string]any)["label"] = "<script>alert(1)</script>"
		}, "markup_not_allowed"},
		{"javascript scheme in greeting", func(m map[string]any) {
			m["intake"].(map[string]any)["greeting"] = "Click javascript:alert(document.cookie)"
		}, "url_not_allowed"},
		{"unknown executor", func(m map[string]any) {
			m["actions"].([]any)[0].(map[string]any)["executor"] = "shell_command"
		}, "schema"},
		{"unknown artifact type", func(m map[string]any) {
			m["navigation"].([]any)[1].(map[string]any)["artifact_type"] = "ghost"
		}, "unknown_artifact_type"},
		{"agent not in bindings", func(m map[string]any) {
			m["actions"].([]any)[0].(map[string]any)["agent_key"] = "sim.unpublished"
		}, "agent_not_bound"},
		{"intake agent not in bindings", func(m map[string]any) {
			m["intake"].(map[string]any)["agent_key"] = "sim.rogue"
		}, "agent_not_bound"},
		{"unknown extension", func(m map[string]any) {
			nav := m["navigation"].([]any)
			m["navigation"] = append(nav, map[string]any{"key": "plugin", "label": "Plugin", "view": "extension", "extension_ref": "acme.remote-widget"})
		}, "unknown_extension"},
		{"unknown permission", func(m map[string]any) {
			m["actions"].([]any)[2].(map[string]any)["required_permission"] = "platform:admin"
		}, "unknown_permission"},
		{"custom web url view", func(m map[string]any) {
			m["navigation"].([]any)[0].(map[string]any)["view"] = "custom_web_url"
		}, "schema"},
		{"raw sql in description", func(m map[string]any) {
			m["description"] = "select * from users where 1=1"
		}, "sql_not_allowed"},
		{"duplicate source mapping", func(m map[string]any) {
			at := m["artifact_types"].([]any)
			at[1].(map[string]any)["source"].(map[string]any)["flowcase_artifact_type"] = "requirement-register"
		}, "duplicate_source"},
		{"malformed rows_path", func(m map[string]any) {
			at := m["artifact_types"].([]any)
			at[0].(map[string]any)["source"].(map[string]any)["rows_path"] = "../etc"
		}, "schema"},
		{"unknown source provider", func(m map[string]any) {
			at := m["artifact_types"].([]any)
			at[0].(map[string]any)["source"].(map[string]any)["provider"] = "remote_http"
		}, "schema"},
		{"unknown flowcase command", func(m map[string]any) {
			m["actions"].([]any)[2].(map[string]any)["command_key"] = "approve_gate"
		}, "unknown_command"},
		{"intake maps two fields to title", func(m map[string]any) {
			f := m["intake"].(map[string]any)["fields"].([]any)
			f[1].(map[string]any)["maps_to"] = "title"
		}, "duplicate_mapping"},
		{"document upload into a non-textarea field", func(m map[string]any) {
			m["intake"].(map[string]any)["documents"] = map[string]any{"target_field": "title", "max_files": 2, "prompt": "Read {filename} please."}
		}, "invalid_document_field"},
		{"document prompt without file name", func(m map[string]any) {
			m["intake"].(map[string]any)["documents"] = map[string]any{"target_field": "rfp_excerpt", "max_files": 2, "prompt": "Read the uploaded file."}
		}, "invalid_document_prompt"},
		{"too many document files", func(m map[string]any) {
			m["intake"].(map[string]any)["documents"] = map[string]any{"target_field": "rfp_excerpt", "max_files": 50, "prompt": "Read {filename} please."}
		}, "schema"},
		{"unknown top-level key", func(m map[string]any) {
			m["on_load"] = "fetch('/x')"
		}, "schema"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := simRaw(t)
			tc.mut(m)
			raw, _ := json.Marshal(m)
			p := FromRaw(raw, "hostile.json")
			if p.Valid() {
				t.Fatal("hostile manifest accepted")
			}
			if !strings.Contains(codes(p), tc.want) {
				t.Fatalf("want %s, got %s", tc.want, codes(p))
			}
		})
	}
}

type memStore struct{ rows []StoredVersion }

func (m *memStore) ListPackVersions(context.Context) ([]StoredVersion, error) { return m.rows, nil }
func (m *memStore) InsertPackVersion(_ context.Context, v StoredVersion) error {
	m.rows = append(m.rows, v)
	return nil
}

func TestChecksumDriftDisablesOnlyThatPack(t *testing.T) {
	st := &memStore{}
	if _, err := Load(context.Background(), packsDir, st, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(st.rows) != 6 {
		t.Fatalf("stored %d versions", len(st.rows))
	}
	for i := range st.rows {
		if st.rows[i].PackKey == "lab.poc-assessment" {
			st.rows[i].Checksum = "sha256:0000"
		}
	}
	reg, err := Load(context.Background(), packsDir, st, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.rows) != 6 {
		t.Fatal("drifted version was re-inserted or overwritten")
	}
	poc := reg.Get("lab.poc-assessment", "1.0.0")
	if poc == nil || poc.Valid() || !strings.Contains(codes(poc), "checksum_drift") {
		t.Fatalf("poc = %+v", poc)
	}
	if reg.LatestPublished("lab.poc-assessment") != nil {
		t.Fatal("invalid pack must not be pinnable")
	}
	if !reg.Get("sim.tender", "1.1.0").Valid() {
		t.Fatal("other packs must stay valid")
	}
}

func TestStoredVersionWithoutFileStaysAvailable(t *testing.T) {
	st := &memStore{}
	if _, err := Load(context.Background(), packsDir, st, nil, nil); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(context.Background(), t.TempDir(), st, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p := reg.Get("sim.tender", "1.0.0"); p == nil || !p.Valid() {
		t.Fatalf("stored pack lost: %+v", p)
	}
}

func TestCanonicalChecksumIgnoresFormatting(t *testing.T) {
	a, _ := Canonical([]byte(`{"b":1, "a":[1,2]}`))
	b, _ := Canonical([]byte("{\n \"a\": [1, 2],\n \"b\": 1\n}"))
	if Checksum(a) != Checksum(b) {
		t.Fatal("formatting changed checksum")
	}
}
