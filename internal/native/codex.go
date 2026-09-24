package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var codexSourceKinds = []string{
	"cli",
	"vscode",
	"exec",
	"appServer",
	"subAgent",
	"subAgentReview",
	"subAgentCompact",
	"subAgentThreadSpawn",
	"subAgentOther",
	"unknown",
}

type codexAdapter struct {
	client codexRPC
}

type codexThread struct {
	ID             string `json:"id"`
	ParentThreadID string `json:"parentThreadId"`
	Preview        string `json:"preview"`
	ModelProvider  string `json:"modelProvider"`
	Model          string `json:"model"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	Cwd            string `json:"cwd"`
	Originator     string `json:"originator"`
	AgentNickname  string `json:"agentNickname"`
	AgentRole      string `json:"agentRole"`
	Name           string `json:"name"`
}

type codexThreadListResponse struct {
	Data       []codexThread `json:"data"`
	NextCursor string        `json:"nextCursor"`
}

type codexThreadReadResponse struct {
	Thread codexThread `json:"thread"`
}

type codexTurn struct {
	ID          string `json:"id"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt"`
	Status      string `json:"status"`
}

type codexTurnListResponse struct {
	Data       []codexTurn `json:"data"`
	NextCursor string      `json:"nextCursor"`
}

type codexItemEnvelope struct {
	TurnID string          `json:"turnId"`
	Item   json.RawMessage `json:"item"`
}

type codexItemListResponse struct {
	Data       []codexItemEnvelope `json:"data"`
	NextCursor string              `json:"nextCursor"`
}

func NewCodex(home string) Adapter {
	binary := strings.TrimSpace(os.Getenv("CLOSEVIEW_CODEX_BIN"))
	if binary == "" {
		binary = "codex"
	}
	return newCodexAdapter(newCodexRPCClient(binary, home))
}

func newCodexAdapter(client codexRPC) *codexAdapter {
	return &codexAdapter{client: client}
}

func (a *codexAdapter) Name() string { return "codex" }

func (a *codexAdapter) List(ctx context.Context) ([]Session, error) {
	sessions := make([]Session, 0)
	cursor := ""
	seen := make(map[string]bool)
	for {
		params := map[string]any{
			"limit":          100,
			"sortKey":        "updated_at",
			"sortDirection":  "desc",
			"archived":       false,
			"sourceKinds":    codexSourceKinds,
			"useStateDbOnly": false,
		}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page codexThreadListResponse
		if err := a.client.Call(ctx, "thread/list", params, &page); err != nil {
			return nil, fmt.Errorf("list Codex threads: %w", err)
		}
		for _, thread := range page.Data {
			sessions = append(sessions, codexSession(thread))
		}
		if page.NextCursor == "" {
			break
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("list Codex threads: app-server repeated pagination cursor")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return sessions, nil
}

func (a *codexAdapter) Get(ctx context.Context, nativeID string) (Detail, error) {
	if strings.TrimSpace(nativeID) == "" {
		return Detail{}, ErrNotFound
	}
	var read codexThreadReadResponse
	if err := a.client.Call(ctx, "thread/read", map[string]any{
		"threadId":     nativeID,
		"includeTurns": false,
	}, &read); err != nil {
		if codexIsNotFound(err) {
			return Detail{}, ErrNotFound
		}
		return Detail{}, fmt.Errorf("read Codex thread: %w", err)
	}

	turns, turnsErr := a.listTurns(ctx, nativeID)
	if turnsErr != nil && ctx.Err() != nil {
		return Detail{}, turnsErr
	}
	items, err := a.listItems(ctx, nativeID)
	if err != nil {
		return Detail{}, err
	}
	detail := codexDetail(read.Thread, turns, items)
	if turnsErr != nil {
		addCodexWarning(&detail, "Codex turn metadata was unavailable; message timestamps may be missing")
	}
	return detail, nil
}

func (a *codexAdapter) Delete(ctx context.Context, nativeID string) error {
	if strings.TrimSpace(nativeID) == "" {
		return ErrNotFound
	}
	var result map[string]any
	if err := a.client.Call(ctx, "thread/delete", map[string]any{
		"threadId": nativeID,
	}, &result); err != nil {
		if codexIsNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("delete Codex thread: %w", err)
	}
	return nil
}

func (a *codexAdapter) Close() error {
	if a.client == nil {
		return nil
	}
	return a.client.Close()
}

func (a *codexAdapter) listTurns(ctx context.Context, threadID string) ([]codexTurn, error) {
	turns := make([]codexTurn, 0)
	cursor := ""
	seen := make(map[string]bool)
	for {
		params := map[string]any{
			"threadId":      threadID,
			"limit":         100,
			"sortDirection": "asc",
			"itemsView":     "notLoaded",
		}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page codexTurnListResponse
		if err := a.client.Call(ctx, "thread/turns/list", params, &page); err != nil {
			if codexIsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("list Codex turns: %w", err)
		}
		turns = append(turns, page.Data...)
		if page.NextCursor == "" {
			return turns, nil
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("list Codex turns: app-server repeated pagination cursor")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}

func (a *codexAdapter) listItems(ctx context.Context, threadID string) ([]codexItemEnvelope, error) {
	items := make([]codexItemEnvelope, 0)
	cursor := ""
	seen := make(map[string]bool)
	for {
		params := map[string]any{
			"threadId":      threadID,
			"limit":         100,
			"sortDirection": "asc",
		}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page codexItemListResponse
		if err := a.client.Call(ctx, "thread/items/list", params, &page); err != nil {
			if codexIsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("list Codex items: %w", err)
		}
		items = append(items, page.Data...)
		if page.NextCursor == "" {
			return items, nil
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("list Codex items: app-server repeated pagination cursor")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}

func codexSession(thread codexThread) Session {
	provider := firstNonEmpty(thread.ModelProvider, "openai")
	return Session{
		NativeID:       thread.ID,
		ThreadID:       thread.ID,
		ParentThreadID: thread.ParentThreadID,
		Title:          titleFallback(thread.Name, cleanPrompt(thread.Preview), thread.Cwd, thread.ID),
		ProjectPath:    thread.Cwd,
		CreatedAt:      codexTime(thread.CreatedAt),
		UpdatedAt:      codexTime(thread.UpdatedAt),
		Provider:       provider,
		Model:          thread.Model,
		Agent:          firstNonEmpty(thread.AgentRole, thread.AgentNickname),
		Origin:         firstNonEmpty(thread.Originator, "Codex"),
	}
}

func codexDetail(thread codexThread, turns []codexTurn, items []codexItemEnvelope) Detail {
	detail := Detail{
		Session:   codexSession(thread),
		Messages:  []Message{},
		ToolCalls: []ToolCall{},
		Warnings:  []string{},
	}
	turnTimes := make(map[string]codexTurn, len(turns))
	for _, turn := range turns {
		turnTimes[turn.ID] = turn
	}
	for _, envelope := range items {
		var item map[string]any
		if err := json.Unmarshal(envelope.Item, &item); err != nil {
			addCodexWarning(&detail, "Skipped a malformed Codex history item")
			continue
		}
		appendCodexItem(&detail, turnTimes[envelope.TurnID], item)
	}
	detail.Session.MessageCount = len(detail.Messages)
	return detail
}

func appendCodexItem(detail *Detail, turn codexTurn, item map[string]any) {
	itemType := codexString(item, "type")
	id := codexString(item, "id")
	model := detail.Session.Model
	provider := detail.Session.Provider
	appendMessage := func(role, content, finish string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		createdAt := codexTime(turn.StartedAt)
		if role == "assistant" || role == "reasoning" {
			createdAt = codexTime(firstNonZero(turn.CompletedAt, turn.StartedAt))
		}
		appendNativeMessage(detail, Message{
			ID:        id,
			Role:      role,
			Content:   content,
			CreatedAt: createdAt,
			Provider:  provider,
			Model:     model,
			Finish:    finish,
		})
	}
	appendTool := func(name, kind, status string, input, output any) {
		if name == "" {
			name = "tool"
		}
		detail.ToolCalls = append(detail.ToolCalls, ToolCall{
			ID:        firstNonEmpty(id, fmt.Sprintf("tool-%d", len(detail.ToolCalls)+1)),
			MessageID: lastAssistantMessage(detail),
			Sequence:  len(detail.ToolCalls) + 1,
			Name:      name,
			Kind:      firstNonEmpty(kind, inferNativeToolKind(name)),
			Status:    codexToolStatus(status),
			Input:     rawText(input),
			Output:    rawText(output),
		})
	}

	switch itemType {
	case "userMessage":
		content := codexContentText(item["content"])
		role := "user"
		if looksLikeContext(content) {
			role = "system"
		}
		appendMessage(role, content, "")
	case "agentMessage":
		appendMessage("assistant", codexString(item, "text"), codexString(item, "phase"))
	case "hookPrompt":
		var fragments []struct {
			Text string `json:"text"`
		}
		if raw, ok := item["fragments"]; ok {
			encoded, _ := json.Marshal(raw)
			_ = json.Unmarshal(encoded, &fragments)
		}
		values := make([]string, 0, len(fragments))
		for _, fragment := range fragments {
			values = append(values, fragment.Text)
		}
		appendMessage("system", strings.Join(values, "\n\n"), "")
	case "reasoning":
		content := strings.Join(append(codexStringList(item["content"]), codexStringList(item["summary"])...), "\n\n")
		appendMessage("reasoning", content, "")
	case "plan":
		appendMessage("assistant", codexString(item, "text"), "plan")
	case "contextCompaction":
		appendMessage("system", "Context compacted", "")
	case "enteredReviewMode":
		appendMessage("system", "Review mode: "+codexString(item, "review"), "")
	case "exitedReviewMode":
		appendMessage("system", "Review complete: "+codexString(item, "review"), "")
	case "subAgentActivity":
		appendMessage("system", fmt.Sprintf("Subagent %s: %s", codexString(item, "kind"), codexString(item, "agentPath")), "")
	case "functionCallOutput":
		name := firstNonEmpty(codexString(item, "namespace"), codexString(item, "name"), "tool")
		appendTool(name, "unknown", "completed", nil, item["output"])
	case "commandExecution":
		output := codexString(item, "aggregatedOutput")
		if output == "" {
			if exitCode, ok := codexInt(item, "exitCode"); ok {
				output = fmt.Sprintf("exit code %d", exitCode)
			}
		}
		appendTool("shell", "shell", codexString(item, "status"), codexString(item, "command"), output)
	case "fileChange":
		appendTool("file", "file_write", codexString(item, "status"), item["changes"], codexString(item, "status"))
	case "mcpToolCall":
		name := strings.Trim(strings.Join([]string{codexString(item, "server"), codexString(item, "tool")}, "/"), "/")
		output := any(item["result"])
		if codexIsEmpty(item["result"]) && !codexIsEmpty(item["error"]) {
			output = item["error"]
		}
		appendTool(name, "mcp", codexString(item, "status"), item["arguments"], output)
	case "dynamicToolCall":
		name := strings.Trim(strings.Join([]string{codexString(item, "namespace"), codexString(item, "tool")}, "/"), "/")
		appendTool(name, "dynamic", codexString(item, "status"), item["arguments"], item["contentItems"])
	case "collabAgentToolCall":
		input := map[string]any{
			"prompt":            codexString(item, "prompt"),
			"receiverThreadIds": item["receiverThreadIds"],
			"senderThreadId":    codexString(item, "senderThreadId"),
		}
		appendTool(codexString(item, "tool"), "agent", codexString(item, "status"), input, item["agentsStates"])
	case "webSearch":
		appendTool("web_search", "search", "completed", codexString(item, "query"), item["results"])
	case "imageView":
		appendTool("image", "file_read", "completed", codexString(item, "path"), nil)
	case "sleep":
		input := map[string]any{"durationMs": item["durationMs"]}
		appendTool("sleep", "wait", "completed", input, "completed")
	case "imageGeneration":
		input := firstNonEmpty(codexString(item, "revisedPrompt"), codexString(item, "prompt"))
		output := any(item["result"])
		if codexIsEmpty(output) {
			output = item["savedPath"]
		}
		appendTool("image_generation", "image", codexString(item, "status"), input, output)
	default:
		if itemType != "" {
			addCodexWarning(detail, "Skipped unsupported Codex item type: "+itemType)
		}
	}
}

func codexContentText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
			continue
		}
		blockType := strings.ToLower(codexString(block, "type"))
		if strings.Contains(blockType, "image") {
			parts = append(parts, "[Image]")
		}
	}
	return strings.Join(parts, "\n\n")
}

func codexStringList(value any) []string {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func codexString(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func codexInt(item map[string]any, key string) (int64, bool) {
	value, ok := item[key].(float64)
	return int64(value), ok
}

func codexIsEmpty(value any) bool {
	if value == nil {
		return true
	}
	raw, err := json.Marshal(value)
	return err != nil || string(raw) == "null" || string(raw) == "{}" || string(raw) == "[]"
}

func codexToolStatus(status string) string {
	switch strings.ToLower(status) {
	case "completed", "succeeded", "success":
		return "completed"
	case "failed", "declined", "interrupted", "errored", "shutdown", "notfound":
		return "failed"
	case "inprogress", "running":
		return "running"
	case "":
		return "unknown"
	default:
		return strings.ToLower(status)
	}
}

func codexTime(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func codexIsNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var rpcErr *codexRPCError
	if errors.As(err, &rpcErr) {
		message := strings.ToLower(rpcErr.Message)
		return strings.Contains(message, "not found") || strings.Contains(message, "no rollout found")
	}
	return false
}

func addCodexWarning(detail *Detail, warning string) {
	if len(detail.Warnings) < 25 {
		detail.Warnings = append(detail.Warnings, warning)
		return
	}
	if len(detail.Warnings) == 25 {
		detail.Warnings = append(detail.Warnings, "Additional Codex history warnings were suppressed")
	}
}

func rawText(value any) string {
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		pretty, _ := json.MarshalIndent(decoded, "", "  ")
		return string(pretty)
	}
	return string(raw)
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
