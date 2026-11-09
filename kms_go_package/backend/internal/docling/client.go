package docling

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

var ErrConversionFailed = errors.New("Docling conversion failed")

type Client struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	pollWait    time.Duration
	maxPollWait time.Duration
}

type Result struct {
	Markdown string
	JSON     json.RawMessage
	Status   string
	Errors   []string
}

func NewClient(baseURL, apiKey string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("Docling base URL is required")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey,
		httpClient: &http.Client{Timeout: 2 * time.Minute}, pollWait: 2 * time.Second, maxPollWait: 30 * time.Minute,
	}, nil
}

func (c *Client) Convert(ctx context.Context, filename, mediaType string, content []byte, taskRecorded func(string) error) (Result, error) {
	taskID, err := c.submit(ctx, filename, mediaType, content)
	if err != nil {
		return Result{}, err
	}
	if taskRecorded != nil {
		if err := taskRecorded(taskID); err != nil {
			return Result{}, fmt.Errorf("record Docling task: %w", err)
		}
	}
	if err := c.wait(ctx, taskID); err != nil {
		return Result{}, err
	}
	return c.fetch(ctx, taskID)
}

func (c *Client) submit(ctx context.Context, filename, mediaType string, content []byte) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("to_formats", "md"); err != nil {
		return "", err
	}
	if err := form.WriteField("to_formats", "json"); err != nil {
		return "", err
	}
	if err := form.WriteField("target", "zip"); err != nil {
		return "", err
	}
	part, err := form.CreateFormFile("files", filepath.Base(filename))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(content); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/convert/file/async", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("submit Docling conversion: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Docling submit returned %s", resp.Status)
	}
	var task struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&task); err != nil {
		return "", fmt.Errorf("decode Docling task: %w", err)
	}
	if task.TaskID == "" {
		return "", errors.New("Docling response omitted task_id")
	}
	return task.TaskID, nil
}

func (c *Client) wait(ctx context.Context, taskID string) error {
	deadline := time.NewTimer(c.maxPollWait)
	defer deadline.Stop()
	ticker := time.NewTicker(c.pollWait)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Docling conversion exceeded maximum wait time")
		case <-ticker.C:
			status, err := c.poll(ctx, taskID)
			if err != nil {
				return err
			}
			switch status {
			case "success", "partial_success":
				return nil
			case "failure", "skipped":
				return fmt.Errorf("%w: task status %s", ErrConversionFailed, status)
			}
		}
	}
}

func (c *Client) poll(ctx context.Context, taskID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/status/poll/"+taskID, nil)
	if err != nil {
		return "", err
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("poll Docling task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Docling poll returned %s", resp.Status)
	}
	var status struct {
		TaskStatus string `json:"task_status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status); err != nil {
		return "", fmt.Errorf("decode Docling status: %w", err)
	}
	return status.TaskStatus, nil
}

func (c *Client) fetch(ctx context.Context, taskID string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/result/"+taskID, nil)
	if err != nil {
		return Result{}, err
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetch Docling result: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("Docling result returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read Docling result: %w", err)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "zip") || bytes.HasPrefix(body, []byte("PK\x03\x04")) {
		return parseZip(body)
	}
	return parseJSON(body)
}

func parseZip(body []byte) (Result, error) {
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return Result{}, fmt.Errorf("read Docling result archive: %w", err)
	}
	var result Result
	for _, file := range r.File {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if ext != ".md" && ext != ".json" {
			continue
		}
		entry, err := file.Open()
		if err != nil {
			return Result{}, err
		}
		data, readErr := io.ReadAll(io.LimitReader(entry, 48<<20))
		closeErr := entry.Close()
		if readErr != nil {
			return Result{}, readErr
		}
		if closeErr != nil {
			return Result{}, closeErr
		}
		switch ext {
		case ".md":
			result.Markdown = string(data)
		case ".json":
			var parsed struct {
				Status   string   `json:"status"`
				Errors   []string `json:"errors"`
				Document struct {
					JSONContent json.RawMessage `json:"json_content"`
				} `json:"document"`
			}
			if err := json.Unmarshal(data, &parsed); err != nil {
				// Some Docling releases return the document JSON directly.
				if !json.Valid(data) {
					return Result{}, fmt.Errorf("decode Docling JSON artifact: %w", err)
				}
				result.JSON = data
			} else {
				result.JSON = parsed.Document.JSONContent
				result.Status = parsed.Status
				result.Errors = parsed.Errors
			}
		}
	}
	if result.Markdown == "" || len(result.JSON) == 0 {
		return Result{}, errors.New("Docling archive is missing Markdown or structured JSON output")
	}
	if result.Status == "failure" {
		return result, fmt.Errorf("%w: %s", ErrConversionFailed, strings.Join(result.Errors, "; "))
	}
	return result, nil
}

func parseJSON(body []byte) (Result, error) {
	var parsed struct {
		Status   string   `json:"status"`
		Errors   []string `json:"errors"`
		Document struct {
			Markdown    string          `json:"md_content"`
			JSONContent json.RawMessage `json:"json_content"`
		} `json:"document"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, fmt.Errorf("decode Docling result: %w", err)
	}
	if parsed.Status == "failure" || parsed.Status == "skipped" {
		return Result{}, fmt.Errorf("%w: %s", ErrConversionFailed, strings.Join(parsed.Errors, "; "))
	}
	if parsed.Document.Markdown == "" || len(parsed.Document.JSONContent) == 0 {
		return Result{}, errors.New("Docling result omitted Markdown or structured JSON output")
	}
	return Result{Markdown: parsed.Document.Markdown, JSON: parsed.Document.JSONContent, Status: parsed.Status, Errors: parsed.Errors}, nil
}
