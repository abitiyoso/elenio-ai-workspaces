package intake

import (
	"strings"
	"testing"

	"github.com/elenio/case-workspace/internal/packs"
)

func TestDetectDocument(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"kak.pdf", "%PDF-1.7\n...", "application/pdf"},
		{"KAK tanpa ekstensi", "%PDF-1.4", "application/pdf"},
		{"rks.docx", "PK\x03\x04rest", docxType},
		{"notes.txt", "Kebutuhan CCTV 310 kamera", "text/plain"},
		{"brief.MD", "# Brief", "text/markdown"},
		{"evil.docx", "MZ\x90\x00", ""},
		{"archive.zip", "PK\x03\x04", ""},
		{"binary.txt", "abc\x00def", ""},
		{"latin1.txt", "caf\xe9", ""},
		{"script.html", "<script>", ""},
	}
	for _, c := range cases {
		got, ok := DetectDocument(c.name, []byte(c.content))
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: got %q %v, want %q", c.name, got, ok, c.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"RKST - Pengadaan Server.pdf":     "RKST - Pengadaan Server.pdf",
		"../../etc/passwd":                "passwd",
		`C:\Users\x\KAK "final".pdf`:      "KAK final.pdf",
		"bad\x00\nname.pdf":               "badname.pdf",
		"   ":                             "",
		"/":                               "",
		strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 196) + ".pdf",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanExtracted(t *testing.T) {
	in := "## RKST\r\n\r\n<!-- image -->\r\nServer &amp; Storage\x07\n\n\n\n- 310 kamera &lt;HD&gt;\n  <!-- image -->  \n"
	want := "## RKST\n\nServer & Storage\n\n- 310 kamera <HD>"
	if got := CleanExtracted(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDocumentText(t *testing.T) {
	a, b := "Lingkup: server VMS", "Garansi 1 tahun"
	docs := []DocumentView{
		{Filename: "kak.pdf", Status: docReady, content: &a},
		{Filename: "gagal.pdf", Status: docFailed},
		{Filename: "lampiran.txt", Status: docReady, content: &b},
	}
	text, cut := documentText(docs, 1000)
	if cut || text != "# kak.pdf\n\nLingkup: server VMS\n\n# lampiran.txt\n\nGaransi 1 tahun" {
		t.Fatalf("text = %q cut=%v", text, cut)
	}
	if text, cut = documentText(docs, 20); !cut || len([]rune(text)) != 20 || !strings.HasSuffix(text, "…") {
		t.Fatalf("truncated = %q cut=%v", text, cut)
	}
	if text, _ = documentText(docs[1:2], 100); text != "" {
		t.Fatalf("failed documents contribute nothing, got %q", text)
	}
}

func TestMergeKeepsLockedField(t *testing.T) {
	fields := []packs.IntakeField{{Key: "title", Widget: "text"}, {Key: "rfp_excerpt", Widget: "textarea", MaxLength: 20000}}
	d := Draft{"rfp_excerpt": "# kak.pdf\n\nfull text"}
	changed := d.Merge(fields, map[string]any{"title": "Server CCTV", "rfp_excerpt": "short summary"}, "rfp_excerpt")
	if len(changed) != 1 || changed[0] != "title" || d["rfp_excerpt"] != "# kak.pdf\n\nfull text" {
		t.Fatalf("changed=%v draft=%v", changed, d)
	}
}
