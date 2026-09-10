package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type codexAdapter struct {
	home string
}

type codexIndexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
}

type codexEnvelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type codexParsed struct {
	detail   Detail
	threadID string
}

func NewCodex(home string) Adapter {
	return &codexAdapter{home: home}
}

func (a *codexAdapter) Name() string { return "codex" }

func (a *codexAdapter) List(ctx context.Context) ([]Session, error) {
	root := filepath.Join(a.home, "sessions")
	paths, err := filesUnder(root, func(path string) bool {
		return strings.HasSuffix(strings.ToLower(path), ".jsonl")
	})
	if err != nil {
		return nil, err
	}
	index, _ := a.readIndex()
	sessions := make([]Session, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		parsed, err := parseCodexRollout(path, false)
		if err != nil {
			continue
		}
		session := parsed.detail.Session
		session.NativeID = filepath.ToSlash(relative)
		session.ThreadID = parsed.threadID
		if entry, ok := index[parsed.threadID]; ok {
			session.Title = titleFallback(entry.ThreadName, session.Title, session.ProjectPath, parsed.threadID)
			if entry.UpdatedAt > session.UpdatedAt {
				session.UpdatedAt = entry.UpdatedAt
			}
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (a *codexAdapter) Get(_ context.Context, nativeID string) (Detail, error) {
	path, err := safeSessionPath(filepath.Join(a.home, "sessions"), nativeID)
	if err != nil {
		return Detail{}, err
	}
	parsed, err := parseCodexRollout(path, true)
	if err != nil {
		if os.IsNotExist(err) {
			return Detail{}, ErrNotFound
		}
		return Detail{}, err
	}
	parsed.detail.Session.NativeID = nativeID
	parsed.detail.Session.ThreadID = parsed.threadID
	index, _ := a.readIndex()
	if entry, ok := index[parsed.threadID]; ok {
		parsed.detail.Session.Title = titleFallback(entry.ThreadName, parsed.detail.Session.Title, parsed.detail.Session.ProjectPath, parsed.threadID)
		if entry.UpdatedAt > parsed.detail.Session.UpdatedAt {
			parsed.detail.Session.UpdatedAt = entry.UpdatedAt
		}
	}
	return parsed.detail, nil
}

func (a *codexAdapter) Delete(ctx context.Context, nativeID string) error {
	root := filepath.Join(a.home, "sessions")
	path, err := safeSessionPath(root, nativeID)
	if err != nil {
		return err
	}
	parsed, err := parseCodexRollout(path, false)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	staged := filepath.Join(filepath.Dir(path), ".closeview-delete-"+filepath.Base(path)+".deleting")
	if err := os.Rename(path, staged); err != nil {
		return err
	}
	restore := true
	defer func() {
		if restore {
			_ = os.Rename(staged, path)
		}
	}()
	remaining, err := a.hasThread(ctx, parsed.threadID)
	if err != nil {
		return err
	}
	if !remaining && parsed.threadID != "" {
		if err := rewriteJSONLinesWithout(filepath.Join(a.home, "session_index.jsonl"), func(line json.RawMessage) bool {
			var entry codexIndexEntry
			return json.Unmarshal(line, &entry) == nil && entry.ID == parsed.threadID
		}); err != nil {
			return err
		}
		if err := rewriteJSONLinesWithout(filepath.Join(a.home, "history.jsonl"), func(line json.RawMessage) bool {
			var entry struct {
				SessionID string `json:"session_id"`
			}
			return json.Unmarshal(line, &entry) == nil && entry.SessionID == parsed.threadID
		}); err != nil {
			return err
		}
	}
	if err := os.Remove(staged); err != nil {
		return err
	}
	restore = false
	return nil
}

func (a *codexAdapter) hasThread(ctx context.Context, threadID string) (bool, error) {
	paths, err := filesUnder(filepath.Join(a.home, "sessions"), func(path string) bool {
		return strings.HasSuffix(strings.ToLower(path), ".jsonl")
	})
	if err != nil {
		return false, err
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		parsed, err := parseCodexRollout(path, false)
		if err == nil && parsed.threadID == threadID {
			return true, nil
		}
	}
	return false, nil
}

func (a *codexAdapter) readIndex() (map[string]codexIndexEntry, error) {
	entries := make(map[string]codexIndexEntry)
	err := jsonLines(filepath.Join(a.home, "session_index.jsonl"), func(line json.RawMessage) error {
		var entry codexIndexEntry
		if json.Unmarshal(line, &entry) == nil && entry.ID != "" {
			entries[entry.ID] = entry
		}
		return nil
	})
	if os.IsNotExist(err) {
		return entries, nil
	}
	return entries, err
}

func parseCodexRollout(path string, includeContent bool) (codexParsed, error) {
	info, err := os.Stat(path)
	if err != nil {
		return codexParsed{}, err
	}
	detail := Detail{Messages: []Message{}, ToolCalls: []ToolCall{}, Warnings: []string{}}
	detail.Session.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	currentModel := ""
	firstPrompt := ""
	fallback := []Message{}
	pendingTools := make(map[string]int)
	err = jsonLines(path, func(line json.RawMessage) error {
		var envelope codexEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			detail.Warnings = append(detail.Warnings, "Skipped a malformed rollout line")
			return nil
		}
		switch envelope.Type {
		case "session_meta":
			var meta struct {
				ID             string          `json:"id"`
				SessionID      string          `json:"session_id"`
				ParentThreadID string          `json:"parent_thread_id"`
				Timestamp      string          `json:"timestamp"`
				CWD            string          `json:"cwd"`
				Originator     string          `json:"originator"`
				Source         json.RawMessage `json:"source"`
				ThreadSource   string          `json:"thread_source"`
			}
			if json.Unmarshal(envelope.Payload, &meta) == nil {
				detail.Session.ThreadID = firstNonEmpty(meta.ID, meta.SessionID)
				detail.Session.ParentThreadID = meta.ParentThreadID
				detail.Session.Agent = codexAgentLabel(meta.Source, meta.ThreadSource)
				detail.Session.CreatedAt = firstNonEmpty(meta.Timestamp, envelope.Timestamp)
				detail.Session.ProjectPath = meta.CWD
				detail.Session.Origin = meta.Originator
			}
		case "turn_context":
			var turn struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(envelope.Payload, &turn) == nil && turn.Model != "" {
				currentModel = turn.Model
				detail.Session.Model = turn.Model
				detail.Session.Provider = "openai"
			}
		case "response_item":
			parseCodexResponse(envelope, includeContent, currentModel, &detail, pendingTools, &firstPrompt)
		case "event_msg":
			if message, ok := codexFallbackMessage(envelope, currentModel); ok {
				fallback = append(fallback, message)
			}
		}
		return nil
	})
	if err != nil {
		return codexParsed{}, err
	}
	if len(detail.Messages) == 0 && includeContent {
		for _, message := range fallback {
			appendNativeMessage(&detail, message)
			if firstPrompt == "" && message.Role == "user" {
				firstPrompt = message.Content
			}
		}
	}
	detail.Session.MessageCount = len(detail.Messages)
	if !includeContent {
		detail.Session.MessageCount = countCodexMessages(path)
	}
	detail.Session.Title = titleFallback("", cleanPrompt(firstPrompt), detail.Session.ProjectPath, detail.Session.ThreadID)
	if detail.Session.ParentThreadID != "" && detail.Session.Agent == "guardian" {
		detail.Session.Title = "Guardian review"
	}
	return codexParsed{detail: detail, threadID: detail.Session.ThreadID}, nil
}

func codexAgentLabel(source json.RawMessage, threadSource string) string {
	var value struct {
		Subagent map[string]string `json:"subagent"`
	}
	if json.Unmarshal(source, &value) == nil {
		for kind, name := range value.Subagent {
			if strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
			if strings.TrimSpace(kind) != "" {
				return strings.TrimSpace(kind)
			}
		}
	}
	if strings.Contains(strings.ToLower(threadSource), "guardian") {
		return "guardian"
	}
	return ""
}

func parseCodexResponse(envelope codexEnvelope, includeContent bool, model string, detail *Detail, pending map[string]int, firstPrompt *string) {
	var item struct {
		ID      string            `json:"id"`
		Type    string            `json:"type"`
		Role    string            `json:"role"`
		Name    string            `json:"name"`
		CallID  string            `json:"call_id"`
		Input   json.RawMessage   `json:"input"`
		Output  json.RawMessage   `json:"output"`
		Content []json.RawMessage `json:"content"`
		Summary []json.RawMessage `json:"summary"`
	}
	if json.Unmarshal(envelope.Payload, &item) != nil {
		return
	}
	switch item.Type {
	case "message":
		content := codexContent(item.Content)
		if content == "" {
			return
		}
		role := normalizeNativeRole(item.Role)
		if role == "user" && looksLikeContext(content) {
			role = "system"
		}
		if *firstPrompt == "" && role == "user" && !looksLikeContext(content) {
			*firstPrompt = content
		}
		if includeContent {
			appendNativeMessage(detail, Message{ID: item.ID, Role: role, Content: content, CreatedAt: envelope.Timestamp, Model: model, Provider: "openai"})
		}
	case "reasoning":
		if !includeContent {
			return
		}
		content := codexContent(item.Summary)
		if content != "" {
			appendNativeMessage(detail, Message{ID: item.ID, Role: "reasoning", Content: content, CreatedAt: envelope.Timestamp, Model: model, Provider: "openai"})
		}
	case "function_call", "custom_tool_call":
		if !includeContent {
			return
		}
		input := rawText(item.Input)
		messageID := lastAssistantMessage(detail)
		call := ToolCall{ID: firstNonEmpty(item.CallID, item.ID), MessageID: messageID, Name: item.Name, Kind: inferNativeToolKind(item.Name), Status: "running", Input: input}
		detail.ToolCalls = append(detail.ToolCalls, call)
		pending[item.CallID] = len(detail.ToolCalls) - 1
	case "function_call_output", "custom_tool_call_output":
		if !includeContent {
			return
		}
		output := rawText(item.Output)
		if index, ok := pending[item.CallID]; ok {
			detail.ToolCalls[index].Output = output
			detail.ToolCalls[index].Status = "completed"
			return
		}
		messageID := lastAssistantMessage(detail)
		detail.ToolCalls = append(detail.ToolCalls, ToolCall{ID: firstNonEmpty(item.CallID, item.ID), MessageID: messageID, Name: "tool", Kind: "unknown", Status: "completed", Output: output})
	}
}

func codexFallbackMessage(envelope codexEnvelope, model string) (Message, bool) {
	var event struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal(envelope.Payload, &event) != nil || event.Message == "" {
		return Message{}, false
	}
	role := ""
	switch event.Type {
	case "user_message":
		role = "user"
	case "agent_message":
		role = "assistant"
	default:
		return Message{}, false
	}
	return Message{Role: role, Content: event.Message, CreatedAt: envelope.Timestamp, Model: model, Provider: "openai"}, true
}

func codexContent(blocks []json.RawMessage) string {
	var values []string
	for _, block := range blocks {
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(block, &content) != nil {
			continue
		}
		switch content.Type {
		case "input_text", "output_text", "text", "summary_text":
			if strings.TrimSpace(content.Text) != "" {
				values = append(values, content.Text)
			}
		case "input_image":
			values = append(values, "[Image]")
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n\n"))
}

func appendNativeMessage(detail *Detail, message Message) {
	message.Sequence = len(detail.Messages) + 1
	if message.ID == "" {
		message.ID = fmt.Sprintf("message-%d", message.Sequence)
	}
	detail.Messages = append(detail.Messages, message)
}

func lastAssistantMessage(detail *Detail) string {
	for index := len(detail.Messages) - 1; index >= 0; index-- {
		if detail.Messages[index].Role == "assistant" {
			return detail.Messages[index].ID
		}
	}
	if len(detail.Messages) > 0 {
		return detail.Messages[len(detail.Messages)-1].ID
	}
	return ""
}

func countCodexMessages(path string) int {
	count := 0
	_ = jsonLines(path, func(line json.RawMessage) error {
		var envelope codexEnvelope
		if json.Unmarshal(line, &envelope) != nil || envelope.Type != "response_item" {
			return nil
		}
		var item struct {
			Type    string            `json:"type"`
			Content []json.RawMessage `json:"content"`
			Summary []json.RawMessage `json:"summary"`
		}
		if json.Unmarshal(envelope.Payload, &item) == nil && ((item.Type == "message" && codexContent(item.Content) != "") || (item.Type == "reasoning" && codexContent(item.Summary) != "")) {
			count++
		}
		return nil
	})
	return count
}

func rawText(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var decoded any
	if json.Unmarshal(value, &decoded) == nil {
		pretty, _ := json.MarshalIndent(decoded, "", "  ")
		return string(pretty)
	}
	return string(value)
}

func safeSessionPath(root, nativeID string) (string, error) {
	if nativeID == "" || filepath.IsAbs(nativeID) {
		return "", fmt.Errorf("invalid session path")
	}
	root = filepath.Clean(root)
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(nativeID)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || !strings.HasSuffix(strings.ToLower(path), ".jsonl") {
		return "", fmt.Errorf("invalid session path")
	}
	return path, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cleanPrompt(value string) string {
	value = strings.TrimSpace(value)
	if looksLikeContext(value) {
		return ""
	}
	if line := strings.Split(value, "\n")[0]; line != "" {
		return line
	}
	return value
}

func looksLikeContext(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "<environment_context>") ||
		strings.HasPrefix(trimmed, "<permissions") ||
		strings.HasPrefix(trimmed, "<recommended_plugins>") ||
		strings.HasPrefix(trimmed, "<skills_instructions>") ||
		strings.HasPrefix(trimmed, "<apps_instructions>") ||
		strings.HasPrefix(trimmed, "<plugins_instructions>") ||
		strings.HasPrefix(trimmed, "# AGENTS.md")
}

func normalizeNativeRole(role string) string {
	switch strings.ToLower(role) {
	case "user", "assistant", "system", "tool":
		return strings.ToLower(role)
	case "developer":
		return "system"
	default:
		return "unknown"
	}
}

func inferNativeToolKind(name string) string {
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "shell"), strings.Contains(name, "exec"), strings.Contains(name, "command"):
		return "shell"
	case strings.Contains(name, "read"):
		return "file_read"
	case strings.Contains(name, "write"), strings.Contains(name, "patch"), strings.Contains(name, "edit"):
		return "file_write"
	case strings.Contains(name, "search"), strings.Contains(name, "find"):
		return "search"
	default:
		return "unknown"
	}
}

func sortMessages(messages []Message) {
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].Sequence < messages[j].Sequence })
}
