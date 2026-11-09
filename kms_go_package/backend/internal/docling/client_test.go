package docling

import (
	"archive/zip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConvertSubmitsPollsAndUnpacksStructuredArtifacts(t *testing.T) {
	var calls int
	client, err := NewClient("http://docling.test", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client.pollWait = time.Millisecond
	client.maxPollWait = time.Second
	client.httpClient.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("X-Api-Key") != "secret" {
			t.Fatal("expected server-side Docling API key")
		}
		switch r.URL.Path {
		case "/v1/convert/file/async":
			if r.Method != http.MethodPost {
				t.Fatalf("method=%s", r.Method)
			}
			reader, err := r.MultipartReader()
			if err != nil {
				t.Fatal(err)
			}
			fileSeen, markdownSeen, jsonSeen := false, false, false
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				value, _ := io.ReadAll(part)
				if part.FormName() == "files" {
					fileSeen = strings.HasSuffix(part.FileName(), "input.pdf") && string(value) == "%PDF-1.7"
				}
				if part.FormName() == "to_formats" && string(value) == "md" {
					markdownSeen = true
				}
				if part.FormName() == "to_formats" && string(value) == "json" {
					jsonSeen = true
				}
			}
			if !fileSeen || !markdownSeen || !jsonSeen {
				t.Fatal("multipart request omitted the PDF or requested output formats")
			}
			return response(r, http.StatusAccepted, `{"task_id":"task-1","task_status":"pending"}`, "application/json"), nil
		case "/v1/status/poll/task-1":
			return response(r, http.StatusOK, `{"task_status":"success"}`, "application/json"), nil
		case "/v1/result/task-1":
			archive := makeArchive(t, map[string]string{
				"result.md":   "# Policy\n\nKeep the recording for 30 days.",
				"result.json": `{"status":"success","document":{"md_content":"# Policy","json_content":{"texts":[{"label":"text","text":"Keep the recording for 30 days.","prov":[{"page_no":3}]}]}}}`,
			})
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/zip"}}, Body: io.NopCloser(strings.NewReader(string(archive))), Request: r}, nil
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
			return nil, nil
		}
	})
	var recorded string
	result, err := client.Convert(context.Background(), "../input.pdf", "application/pdf", []byte("%PDF-1.7"), func(taskID string) error { recorded = taskID; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if recorded != "task-1" || result.Status != "success" || !strings.Contains(result.Markdown, "30 days") || !strings.Contains(string(result.JSON), "page_no") {
		t.Fatalf("recorded=%q result=%+v", recorded, result)
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want submit+poll+result", calls)
	}
}

func TestParseJSONFailureIsReported(t *testing.T) {
	_, err := parseJSON([]byte(`{"status":"failure","errors":["unsupported document"],"document":{"md_content":"","json_content":{}}}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported document") {
		t.Fatalf("err=%v", err)
	}
}

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer strings.Builder
	writer := zip.NewWriter(&stringWriter{builder: &buffer})
	for name, value := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return []byte(buffer.String())
}

type stringWriter struct{ builder *strings.Builder }

func (w *stringWriter) Write(p []byte) (int, error) { return w.builder.Write(p) }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(r *http.Request, status int, body, contentType string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
