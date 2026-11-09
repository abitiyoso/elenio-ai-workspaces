// Package docling converts uploaded intake documents (PDF, DOCX) to Markdown through docling-serve's
// asynchronous API, with OCR for scanned pages. One call submits; status and result are fetched by task id
// on later polls, so a conversion survives a BFF restart and never holds an HTTP request open.
package docling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Task states as docling-serve reports them, reduced to what the intake needs.
const (
	StatePending = "pending"
	StateSuccess = "success"
	StateFailure = "failure"
)

var (
	// ErrUnknownTask: docling-serve no longer knows the task (restarted or expired).
	ErrUnknownTask = errors.New("docling task not found")
	// ErrFailed: the conversion ran and failed (unreadable or unsupported file).
	ErrFailed = errors.New("docling conversion failed")

	taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,128}$`)
)

const maxResult = 32 << 20

// Converter is what the intake uses; tests replace it.
type Converter interface {
	Submit(ctx context.Context, filename string, content []byte) (taskID string, err error)
	Status(ctx context.Context, taskID string) (string, error)
	Markdown(ctx context.Context, taskID string) (string, error)
}

type Client struct {
	base   string
	apiKey string
	http   *http.Client
	// OCRLangs are EasyOCR language codes for scanned pages.
	OCRLangs []string
}

func New(baseURL, apiKey string, timeout time.Duration, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("docling: base URL must be an absolute http(s) URL")
	}
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: hc, OCRLangs: []string{"id", "en"}}, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	return c.http.Do(req)
}

// Submit starts an OCR-enabled conversion to Markdown with images left as placeholders.
func (c *Client) Submit(ctx context.Context, filename string, content []byte) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fields := [][2]string{{"to_formats", "md"}, {"do_ocr", "true"}, {"image_export_mode", "placeholder"}, {"abort_on_error", "false"}}
	for _, l := range c.OCRLangs {
		fields = append(fields, [2]string{"ocr_lang", l})
	}
	for _, f := range fields {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return "", err
		}
	}
	part, err := form.CreateFormFile("files", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(content); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/convert/file/async", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("docling submit: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("docling submit: status %d", resp.StatusCode)
	}
	var task struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&task); err != nil || !taskIDPattern.MatchString(task.TaskID) {
		return "", errors.New("docling submit: response has no valid task_id")
	}
	return task.TaskID, nil
}

// Status polls a task once: pending, success or failure.
func (c *Client) Status(ctx context.Context, taskID string) (string, error) {
	if !taskIDPattern.MatchString(taskID) {
		return "", ErrUnknownTask
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/status/poll/"+taskID, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("docling status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrUnknownTask
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("docling status: status %d", resp.StatusCode)
	}
	var st struct {
		TaskStatus string `json:"task_status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return "", fmt.Errorf("docling status: %w", err)
	}
	switch st.TaskStatus {
	case "success", "partial_success":
		return StateSuccess, nil
	case "failure", "skipped":
		return StateFailure, nil
	}
	return StatePending, nil
}

// Markdown fetches a finished task's Markdown.
func (c *Client) Markdown(ctx context.Context, taskID string) (string, error) {
	if !taskIDPattern.MatchString(taskID) {
		return "", ErrUnknownTask
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/result/"+taskID, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("docling result: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrUnknownTask
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("docling result: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResult+1))
	if err != nil {
		return "", fmt.Errorf("docling result: %w", err)
	}
	if len(raw) > maxResult {
		return "", fmt.Errorf("%w: result larger than %d MiB", ErrFailed, maxResult>>20)
	}
	var out struct {
		Status   string `json:"status"`
		Errors   []any  `json:"errors"`
		Document struct {
			Markdown string `json:"md_content"`
		} `json:"document"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("docling result: %w", err)
	}
	if out.Status == "failure" || out.Status == "skipped" {
		return "", fmt.Errorf("%w: status %s", ErrFailed, out.Status)
	}
	return out.Document.Markdown, nil
}
