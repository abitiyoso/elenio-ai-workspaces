package chunking

import "testing"

func TestNormalizeKeepsRealProvenanceAndStableIDs(t *testing.T) {
	json := []byte(`{"texts":[{"label":"section_header","level":1,"text":"Retention"},{"label":"text","text":"Keep the recording for 30 days.","prov":[{"page_no":4}]},{"label":"table","text":"Region | Days\nNorth | 30","prov":[{"page_no":5}]}]}`)
	one, err := Normalize(json, "", "version-1", 256)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Normalize(json, "", "version-1", 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != len(two) || one[0].ID != two[0].ID {
		t.Fatal("chunk IDs must be stable for the same immutable version and text")
	}
	if one[0].PageStart == nil || *one[0].PageStart != 4 || one[0].PageEnd == nil || *one[0].PageEnd != 5 {
		t.Fatalf("page provenance=%v-%v", one[0].PageStart, one[0].PageEnd)
	}
	if len(one[0].HeadingPath) != 1 || one[0].HeadingPath[0] != "Retention" {
		t.Fatalf("heading path=%v", one[0].HeadingPath)
	}
}

func TestNormalizeDoesNotInventPageNumbers(t *testing.T) {
	chunks, err := Normalize(nil, "# Intro\n\nUnpaginated text", "version-1", 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].PageStart != nil || chunks[0].PageEnd != nil {
		t.Fatalf("unexpected inferred page numbers: %+v", chunks)
	}
}

func TestNormalizeReturnsNonNilEmptyHeadingPathWhenAbsent(t *testing.T) {
	chunks, err := Normalize([]byte(`{"texts":[{"label":"text","text":"Text without a heading."}]}`), "", "version-1", 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected one chunk, got %d", len(chunks))
	}
	if chunks[0].HeadingPath == nil {
		t.Fatal("headingless chunk must use a non-nil empty heading path for PostgreSQL TEXT[] NOT NULL")
	}
	if len(chunks[0].HeadingPath) != 0 {
		t.Fatalf("heading path=%v, want empty", chunks[0].HeadingPath)
	}
}

func TestNormalizeSplitsLongText(t *testing.T) {
	text := make([]byte, 1800)
	for i := range text {
		text[i] = 'a'
	}
	chunks, err := Normalize(nil, string(text), "version-1", 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected split chunks, got %d", len(chunks))
	}
}
