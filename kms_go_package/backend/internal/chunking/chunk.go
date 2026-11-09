package chunking

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Chunk struct {
	ID           string
	Number       int
	Content      string
	HeadingPath  []string
	PageStart    *int
	PageEnd      *int
	SourceAnchor string
}

type block struct {
	text        string
	label       string
	headingPath []string
	page        *int
}

func Normalize(doclingJSON []byte, markdown string, versionID string, maxChars int) ([]Chunk, error) {
	if versionID == "" {
		return nil, fmt.Errorf("version ID is required")
	}
	if maxChars < 128 {
		maxChars = 1200
	}
	blocks, err := blocksFromJSON(doclingJSON)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		blocks = blocksFromMarkdown(markdown)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("Docling output contained no text blocks")
	}
	var output []Chunk
	var current strings.Builder
	var path []string
	var pageStart, pageEnd *int
	flush := func() {
		content := strings.TrimSpace(current.String())
		if content == "" {
			return
		}
		number := len(output)
		sections := make([]string, len(path))
		copy(sections, path)
		anchor := ""
		if pageStart != nil && pageEnd != nil {
			if *pageStart == *pageEnd {
				anchor = "page:" + strconv.Itoa(*pageStart)
			} else {
				anchor = fmt.Sprintf("pages:%d-%d", *pageStart, *pageEnd)
			}
		}
		if len(sections) > 0 {
			if anchor != "" {
				anchor += "#"
			}
			anchor += "section:" + strings.Join(sections, " / ")
		}
		seed := fmt.Sprintf("%s\x00%d\x00%s", versionID, number, content)
		sum := sha256.Sum256([]byte(seed))
		output = append(output, Chunk{ID: hex.EncodeToString(sum[:]), Number: number, Content: content, HeadingPath: sections, PageStart: cloneInt(pageStart), PageEnd: cloneInt(pageEnd), SourceAnchor: anchor})
		current.Reset()
		pageStart, pageEnd = nil, nil
	}
	appendText := func(text string, page *int) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for len(text) > 0 {
			space := maxChars - utf8.RuneCountInString(current.String())
			if space <= 0 {
				flush()
				space = maxChars
			}
			remaining := utf8.RuneCountInString(text)
			if remaining <= space {
				markPage(page, &pageStart, &pageEnd)
				if current.Len() > 0 {
					current.WriteByte(' ')
				}
				current.WriteString(text)
				return
			}
			runes := []rune(text)
			prefix := string(runes[:space])
			cut := space
			if lastSpace := strings.LastIndex(prefix, " "); lastSpace > 0 {
				cut = utf8.RuneCountInString(prefix[:lastSpace])
			}
			markPage(page, &pageStart, &pageEnd)
			if current.Len() > 0 {
				current.WriteByte(' ')
			}
			current.WriteString(string(runes[:cut]))
			flush()
			text = strings.TrimSpace(string(runes[cut:]))
		}
	}
	for _, item := range blocks {
		if isHeading(item.label) {
			if current.Len() > 0 {
				flush()
			}
			path = item.headingPath
			continue
		}
		if current.Len() > 0 && strings.Join(path, "\x00") != strings.Join(item.headingPath, "\x00") {
			flush()
		}
		path = item.headingPath
		appendText(item.text, item.page)
	}
	flush()
	return output, nil
}

func markPage(page *int, start, end **int) {
	if page == nil {
		return
	}
	if *start == nil || *page < **start {
		*start = cloneInt(page)
	}
	if *end == nil || *page > **end {
		*end = cloneInt(page)
	}
}

func blocksFromJSON(raw []byte) ([]block, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode structured Docling output: %w", err)
	}
	if nested, ok := root["document"].(map[string]any); ok {
		root = nested
	}
	texts, ok := root["texts"].([]any)
	if !ok {
		return nil, nil
	}
	var result []block
	var headings []string
	for _, value := range texts {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		label, _ := item["label"].(string)
		text, _ := item["text"].(string)
		if strings.TrimSpace(text) == "" {
			text, _ = item["orig"].(string)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if isHeading(label) {
			level := intField(item["level"])
			if level < 1 {
				level = 1
			}
			if level <= len(headings) {
				headings = headings[:level-1]
			}
			headings = append(headings, text)
			result = append(result, block{text: text, label: label, headingPath: append([]string(nil), headings...), page: provenancePage(item["prov"])})
			continue
		}
		result = append(result, block{text: text, label: label, headingPath: append([]string(nil), headings...), page: provenancePage(item["prov"])})
	}
	return result, nil
}

func blocksFromMarkdown(markdown string) []block {
	var result []block
	var headings []string
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, "#")
		if len(trimmed) < len(line) && strings.HasPrefix(trimmed, " ") {
			level := len(line) - len(trimmed)
			heading := strings.TrimSpace(trimmed)
			if level <= len(headings) {
				headings = headings[:level-1]
			}
			headings = append(headings, heading)
			result = append(result, block{text: heading, label: "section_header", headingPath: append([]string(nil), headings...)})
			continue
		}
		result = append(result, block{text: line, headingPath: append([]string(nil), headings...)})
	}
	return result
}

func provenancePage(value any) *int {
	provenance, ok := value.([]any)
	if !ok {
		return nil
	}
	var page *int
	for _, item := range provenance {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidate := intField(entry["page_no"])
		if candidate < 1 {
			candidate = intField(entry["page"])
		}
		if candidate > 0 && (page == nil || candidate < *page) {
			page = &candidate
		}
	}
	return page
}

func intField(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	case int:
		return number
	default:
		return 0
	}
}

func isHeading(label string) bool {
	label = strings.ToLower(strings.ReplaceAll(label, "-", "_"))
	return label == "section_header" || label == "title" || label == "heading"
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
