package generic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Dylan-Liew/closeview/internal/parser/common"
	"github.com/Dylan-Liew/closeview/internal/store"
)

type Options struct {
	Source      string
	Title       string
	ProjectPath string
}

func ParseFile(path string, opts Options) (store.NewSession, []string, error) {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return store.NewSession{}, nil, err
	}
	content := string(contentBytes)
	source := opts.Source
	if source == "" || source == "auto" {
		source = detectSource(path, content)
	}
	title := opts.Title
	if title == "" {
		title = inferTitle(path, content)
	}

	messages, warnings := parseMessages(path, content)
	if len(messages) == 0 {
		messages = []store.NewMessage{{Role: "unknown", Content: content}}
		warnings = append(warnings, path+": imported as a single unstructured message")
	}
	messages = common.EnrichMessages(messages)
	parseEvents := make([]store.NewParseEvent, 0, len(warnings))
	for _, warning := range warnings {
		parseEvents = append(parseEvents, store.NewParseEvent{Severity: "warning", Code: "parse_warning", Message: warning})
	}

	return store.NewSession{
		Source:      source,
		Title:       title,
		ProjectPath: opts.ProjectPath,
		RawPath:     path,
		SourceHash:  common.SHA256Hex(contentBytes),
		RawContent:  content,
		Messages:    messages,
		ParseEvents: parseEvents,
	}, warnings, nil
}

func parseMessages(path string, content string) ([]store.NewMessage, []string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		messages, err := parseJSONMessages(content)
		if err == nil {
			return messages, nil
		}
		return nil, []string{path + ": JSON parser fallback: " + err.Error()}
	case ".jsonl", ".ndjson":
		messages, err := parseJSONLMessages(content)
		if err == nil {
			return messages, nil
		}
		return nil, []string{path + ": JSONL parser fallback: " + err.Error()}
	default:
		return parseMarkdownMessages(content), nil
	}
}

func parseJSONMessages(content string) ([]store.NewMessage, error) {
	var root any
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		return nil, err
	}
	if array, ok := root.([]any); ok {
		return messagesFromArray(array), nil
	}
	object, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("root is not an object or array")
	}
	for _, key := range []string{"messages", "conversation", "transcript", "items"} {
		if array, ok := object[key].([]any); ok {
			return messagesFromArray(array), nil
		}
	}
	pretty, _ := json.MarshalIndent(root, "", "  ")
	return []store.NewMessage{{Role: "unknown", Content: string(pretty)}}, nil
}

func parseJSONLMessages(content string) ([]store.NewMessage, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 1024), 1024*1024*8)
	var messages []store.NewMessage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			return nil, err
		}
		messages = append(messages, messageFromObject(object))
	}
	return messages, scanner.Err()
}

func messagesFromArray(array []any) []store.NewMessage {
	var messages []store.NewMessage
	for _, item := range array {
		object, ok := item.(map[string]any)
		if !ok {
			bytes, _ := json.MarshalIndent(item, "", "  ")
			messages = append(messages, store.NewMessage{Role: "unknown", Content: string(bytes)})
			continue
		}
		messages = append(messages, messageFromObject(object))
	}
	return messages
}

func messageFromObject(object map[string]any) store.NewMessage {
	role := stringValue(object, "role", "type", "author")
	content := stringValue(object, "content", "text", "message", "body")
	toolCalls := toolCallsFromObject(object)
	if content == "" {
		bytes, _ := json.MarshalIndent(object, "", "  ")
		content = string(bytes)
	}
	return store.NewMessage{Role: common.NormalizeRole(role), Content: content, ToolCalls: toolCalls}
}

func toolCallsFromObject(object map[string]any) []store.NewToolCall {
	var calls []store.NewToolCall
	for _, key := range []string{"tool_calls", "toolCalls", "tools"} {
		array, ok := object[key].([]any)
		if !ok {
			continue
		}
		for _, item := range array {
			tool, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := stringValue(tool, "name", "tool", "function")
			input := stringValue(tool, "input", "arguments", "args", "command")
			output := stringValue(tool, "output", "result", "content")
			calls = append(calls, store.NewToolCall{Name: name, Kind: common.InferToolKind(name, input), Status: stringValue(tool, "status"), Input: input, Output: output})
		}
	}
	if command := stringValue(object, "command", "cmd"); command != "" {
		calls = append(calls, store.NewToolCall{Name: "shell", Kind: "shell", Status: stringValue(object, "status"), Input: command, Output: stringValue(object, "output", "stdout", "stderr")})
	}
	return calls
}

var roleHeadingRE = regexp.MustCompile(`(?i)^#{1,6}\s*(user|assistant|system|tool)\b|^\*\*(user|assistant|system|tool)\*\*:?|^(user|assistant|system|tool):\s*$`)

func parseMarkdownMessages(content string) []store.NewMessage {
	lines := strings.Split(content, "\n")
	var messages []store.NewMessage
	role := "unknown"
	var block []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(block, "\n"))
		if text != "" {
			messages = append(messages, store.NewMessage{Role: role, Content: text})
		}
		block = nil
	}
	for _, line := range lines {
		if match := roleHeadingRE.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			flush()
			for _, group := range match[1:] {
				if group != "" {
					role = common.NormalizeRole(strings.ToLower(group))
					break
				}
			}
			continue
		}
		block = append(block, line)
	}
	flush()
	return messages
}

func detectSource(path string, content string) string {
	lower := strings.ToLower(path + "\n" + content[:min(len(content), 4096)])
	switch {
	case strings.Contains(lower, "opencode"):
		return "opencode"
	case strings.Contains(lower, "claude"):
		return "claude-code"
	case strings.Contains(lower, "codex"):
		return "codex"
	case strings.Contains(lower, "copilot"):
		return "copilot"
	default:
		return "unknown"
	}
}

func inferTitle(path string, content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func IsSupportedFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".json", ".jsonl", ".ndjson", ".txt", ".log", ".db", ".sqlite":
		return true
	default:
		return false
	}
}

func IsSQLiteFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	default:
		return false
	}
}

func stringValue(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			switch typed := value.(type) {
			case string:
				return typed
			case map[string]any, []any:
				bytes, _ := json.MarshalIndent(typed, "", "  ")
				return string(bytes)
			}
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
