package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// DevMockModel is a deterministic stand-in used only when the server runs in
// development and no approved model is bound. It exercises the real Eino
// agent loop, tools and validators; runs are labelled model_mode=dev_mock.
type DevMockModel struct {
	outputSchema json.RawMessage
	tools        []*schema.ToolInfo
}

func NewDevMockModel(outputSchema json.RawMessage) *DevMockModel {
	return &DevMockModel{outputSchema: outputSchema}
}

func (m *DevMockModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &DevMockModel{outputSchema: m.outputSchema, tools: tools}, nil
}

func (m *DevMockModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	tools := m.tools
	// The ADK passes tools per call as an option rather than via WithTools.
	if common := model.GetCommonOptions(nil, opts...); common != nil && len(common.Tools) > 0 {
		tools = common.Tools
	}
	var query string
	var evidence []Evidence
	toolResults := 0
	for _, msg := range input {
		switch msg.Role {
		case schema.User:
			query = msg.Content
		case schema.Tool:
			toolResults++
			var result struct {
				Results []Evidence `json:"results"`
			}
			if json.Unmarshal([]byte(msg.Content), &result) == nil {
				evidence = append(evidence, result.Results...)
			}
		}
	}
	if toolResults == 0 {
		for _, info := range tools {
			if strings.HasSuffix(info.Name, "search") {
				args, _ := json.Marshal(map[string]any{"query": mockQuery(query), "top_k": 5})
				return schema.AssistantMessage("", []schema.ToolCall{{ID: "dev-mock-call-1", Type: "function",
					Function: schema.FunctionCall{Name: info.Name, Arguments: string(args)}}}), nil
			}
		}
	}
	// Prefer approved sources so the draft demonstrates grounded citation.
	sort.SliceStable(evidence, func(i, j int) bool {
		return evidence[i].TrustStatus == "approved" && evidence[j].TrustStatus != "approved"
	})
	ids := make([]string, 0, len(evidence))
	for _, e := range evidence {
		ids = append(ids, e.SourceID)
	}
	var root map[string]any
	_ = json.Unmarshal(m.outputSchema, &root)
	value := mockValue(root, "output", ids, 0)
	content, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return schema.AssistantMessage(string(content), nil), nil
}

func (m *DevMockModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

// mockQuery pulls a short search query out of the run input block.
func mockQuery(userMessage string) string {
	text := userMessage
	if i := strings.Index(text, "```json"); i >= 0 {
		text = text[i+7:]
		if j := strings.Index(text, "```"); j >= 0 {
			text = text[:j]
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[:200]
	}
	if text == "" {
		text = "requirements"
	}
	return text
}

func mockValue(node map[string]any, name string, ids []string, depth int) any {
	if depth > 6 || node == nil {
		return nil
	}
	if c, ok := node["const"]; ok {
		return c
	}
	if enum, ok := node["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	typ, _ := node["type"].(string)
	if list, ok := node["type"].([]any); ok && len(list) > 0 {
		typ, _ = list[0].(string)
	}
	lower := strings.ToLower(name)
	switch typ {
	case "object":
		out := map[string]any{}
		props, _ := node["properties"].(map[string]any)
		required := map[string]bool{}
		if req, ok := node["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required[s] = true
				}
			}
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// An object whose properties are all optional (for example a partial
		// field update) is filled completely so the mock still shows its shape.
		fillAll := len(required) == 0
		for _, key := range keys {
			if !fillAll && !required[key] && !evidenceKeys[key] {
				continue
			}
			child, _ := props[key].(map[string]any)
			if v := mockValue(child, key, ids, depth+1); v != nil {
				out[key] = v
			}
		}
		return out
	case "array":
		items, _ := node["items"].(map[string]any)
		if evidenceKeys[lower] {
			list := []any{}
			for i, id := range ids {
				if i >= 2 {
					break
				}
				list = append(list, id)
			}
			return list
		}
		count := 1
		if min, ok := node["minItems"].(float64); ok && int(min) > count {
			count = int(min)
		}
		list := make([]any, 0, count)
		for i := 0; i < count; i++ {
			list = append(list, mockValue(items, name, ids, depth+1))
		}
		return list
	case "integer", "number":
		if min, ok := node["minimum"].(float64); ok {
			return min
		}
		return 0
	case "boolean":
		return false
	case "string":
		if strings.Contains(lower, "source_id") && len(ids) > 0 {
			return ids[0]
		}
		if pattern, ok := node["pattern"].(string); ok {
			if sample := sampleForPattern(pattern); sample != "" {
				return sample
			}
		}
		if min, ok := node["minLength"].(float64); ok && min > 40 {
			return strings.Repeat("x", int(min))
		}
		return fmt.Sprintf("[dev-mock] %s", name)
	default:
		return nil
	}
}

var digitRun = regexp.MustCompile(`\[0-9\]\{(\d+)\}|\[0-9\]\+|\\d`)

// sampleForPattern produces a value for simple id patterns such as ^R-[0-9]{3}$.
func sampleForPattern(pattern string) string {
	candidate := strings.TrimSuffix(strings.TrimPrefix(pattern, "^"), "$")
	candidate = digitRun.ReplaceAllStringFunc(candidate, func(match string) string {
		if sub := digitRun.FindStringSubmatch(match); len(sub) > 1 && sub[1] != "" {
			n, _ := strconv.Atoi(sub[1])
			if n < 1 || n > 12 {
				return "1"
			}
			return strings.Repeat("0", n-1) + "1"
		}
		return "1"
	})
	if re, err := regexp.Compile(pattern); err == nil && re.MatchString(candidate) {
		return candidate
	}
	return ""
}
