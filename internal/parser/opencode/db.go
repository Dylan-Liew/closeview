package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Dylan-Liew/closeview/internal/parser/common"
	"github.com/Dylan-Liew/closeview/internal/store"
	_ "modernc.org/sqlite"
)

const Source = "opencode"

type Options struct {
	Title       string
	ProjectPath string
}

type sourceSession struct {
	ID        string
	Title     string
	Directory string
	Agent     string
	Model     string
	Created   int64
	Updated   int64
}

type sourceMessage struct {
	ID      string
	Role    string
	Created int64
	Data    string
}

type messageMetadata struct {
	Provider         string
	Model            string
	Finish           string
	Cost             float64
	TokensInput      int
	TokensOutput     int
	TokensReasoning  int
	TokensCacheRead  int
	TokensCacheWrite int
}

func ParseDB(ctx context.Context, path string, opts Options) ([]store.NewSession, []string, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `select id, coalesce(title, ''), coalesce(directory, ''), coalesce(agent, ''), coalesce(model, ''), coalesce(time_created, 0), coalesce(time_updated, 0) from session order by time_updated desc`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var sessions []store.NewSession
	var warnings []string
	for rows.Next() {
		var source sourceSession
		if err := rows.Scan(&source.ID, &source.Title, &source.Directory, &source.Agent, &source.Model, &source.Created, &source.Updated); err != nil {
			return sessions, warnings, err
		}
		session, sessionWarnings, err := parseSession(ctx, db, path, source, opts)
		warnings = append(warnings, sessionWarnings...)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", source.ID, err))
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, warnings, rows.Err()
}

func parseSession(ctx context.Context, db *sql.DB, dbPath string, source sourceSession, opts Options) (store.NewSession, []string, error) {
	messageRows, err := db.QueryContext(ctx, `select id, coalesce(json_extract(data,'$.role'), ''), coalesce(time_created, 0), data from message where session_id = ? order by time_created, id`, source.ID)
	if err != nil {
		return store.NewSession{}, nil, err
	}
	defer messageRows.Close()

	var messages []store.NewMessage
	var warnings []string
	for messageRows.Next() {
		var message sourceMessage
		if err := messageRows.Scan(&message.ID, &message.Role, &message.Created, &message.Data); err != nil {
			return store.NewSession{}, warnings, err
		}
		parsed, err := parseMessageParts(ctx, db, source.ID, message)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s/%s: %v", source.ID, message.ID, err))
			continue
		}
		messages = append(messages, parsed)
	}
	if err := messageRows.Err(); err != nil {
		return store.NewSession{}, warnings, err
	}
	if len(messages) == 0 {
		warnings = append(warnings, source.ID+": OpenCode session has no messages")
	}
	messages = common.EnrichMessages(messages)

	parseEvents := make([]store.NewParseEvent, 0, len(warnings))
	for _, warning := range warnings {
		parseEvents = append(parseEvents, store.NewParseEvent{Severity: "warning", Code: "opencode_parse_warning", Message: warning})
	}
	title := source.Title
	if opts.Title != "" {
		title = opts.Title
	}
	projectPath := opts.ProjectPath
	if projectPath == "" {
		projectPath = source.Directory
	}
	return store.NewSession{
		Source:      Source,
		Provider:    sessionProvider(source.Model),
		Model:       sessionModel(source.Model),
		Agent:       source.Agent,
		Title:       title,
		ProjectPath: projectPath,
		CreatedAt:   formatOpenCodeTime(source.Created),
		RawPath:     dbPath + "#" + source.ID,
		SourceHash:  "opencode-db:" + source.ID,
		RawContent:  "OpenCode SQLite session " + source.ID + " updated " + fmt.Sprint(source.Updated),
		Messages:    messages,
		ParseEvents: parseEvents,
	}, warnings, nil
}

func parseMessageParts(ctx context.Context, db *sql.DB, sessionID string, message sourceMessage) (store.NewMessage, error) {
	rows, err := db.QueryContext(ctx, `select data from part where session_id = ? and message_id = ? order by time_created, id`, sessionID, message.ID)
	if err != nil {
		return store.NewMessage{}, err
	}
	defer rows.Close()

	var content []string
	var toolCalls []store.NewToolCall
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return store.NewMessage{}, err
		}
		text, call := parsePart(data)
		if text != "" {
			content = append(content, text)
		}
		if call.Name != "" || call.Input != "" || call.Output != "" {
			toolCalls = append(toolCalls, call)
		}
	}
	if err := rows.Err(); err != nil {
		return store.NewMessage{}, err
	}
	metadata := parseMessageMetadata(message.Data)
	return store.NewMessage{
		CreatedAt:        formatOpenCodeTime(message.Created),
		Role:             common.NormalizeRole(message.Role),
		Content:          strings.TrimSpace(strings.Join(content, "\n\n")),
		Provider:         metadata.Provider,
		Model:            metadata.Model,
		Finish:           metadata.Finish,
		Cost:             metadata.Cost,
		TokensInput:      metadata.TokensInput,
		TokensOutput:     metadata.TokensOutput,
		TokensReasoning:  metadata.TokensReasoning,
		TokensCacheRead:  metadata.TokensCacheRead,
		TokensCacheWrite: metadata.TokensCacheWrite,
		ToolCalls:        toolCalls,
	}, nil
}

func formatOpenCodeTime(value int64) string {
	if value <= 0 {
		return ""
	}
	var t time.Time
	switch {
	case value > 1e17:
		t = time.Unix(0, value)
	case value > 1e14:
		t = time.Unix(0, value*1000)
	case value > 1e11:
		t = time.UnixMilli(value)
	default:
		t = time.Unix(value, 0)
	}
	return t.UTC().Format(time.RFC3339)
}

func parsePart(data string) (string, store.NewToolCall) {
	var part map[string]any
	if err := json.Unmarshal([]byte(data), &part); err != nil {
		return data, store.NewToolCall{}
	}
	switch stringValue(part, "type") {
	case "text":
		return stringValue(part, "text"), store.NewToolCall{}
	case "reasoning":
		text := stringValue(part, "text")
		if text == "" {
			text = stringValue(part, "summary")
		}
		if text == "" {
			return "", store.NewToolCall{}
		}
		return "Reasoning:\n" + text, store.NewToolCall{}
	case "tool":
		toolName := stringValue(part, "tool")
		state, _ := part["state"].(map[string]any)
		input := ""
		output := ""
		status := "unknown"
		if state != nil {
			status = stringValue(state, "status")
			input = stringValue(state, "input")
			if inputMap, ok := state["input"].(map[string]any); ok {
				input = stringValue(inputMap, "command", "description", "filePath", "pattern")
				if input == "" {
					bytes, _ := json.MarshalIndent(inputMap, "", "  ")
					input = string(bytes)
				}
			}
			output = stringValue(state, "output", "error")
			if output == "" {
				if metadata, ok := state["metadata"].(map[string]any); ok {
					output = stringValue(metadata, "output")
				}
			}
		}
		return "", store.NewToolCall{Name: toolName, Kind: common.InferToolKind(toolName, input), Status: status, Input: input, Output: output}
	default:
		return "", store.NewToolCall{}
	}
}

func parseMessageMetadata(data string) messageMetadata {
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return messageMetadata{}
	}
	metadata := messageMetadata{
		Provider: stringValue(raw, "providerID"),
		Model:    stringValue(raw, "modelID"),
		Finish:   stringValue(raw, "finish"),
		Cost:     floatValue(raw, "cost"),
	}
	if tokens, ok := raw["tokens"].(map[string]any); ok {
		metadata.TokensInput = intValue(tokens, "input")
		metadata.TokensOutput = intValue(tokens, "output")
		metadata.TokensReasoning = intValue(tokens, "reasoning")
		if cache, ok := tokens["cache"].(map[string]any); ok {
			metadata.TokensCacheRead = intValue(cache, "read")
			metadata.TokensCacheWrite = intValue(cache, "write")
		}
	}
	return metadata
}

func sessionModel(data string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return data
	}
	return stringValue(raw, "id")
}

func sessionProvider(data string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return ""
	}
	return stringValue(raw, "providerID")
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

func intValue(object map[string]any, key string) int {
	switch value := object[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return 0
}

func floatValue(object map[string]any, key string) float64 {
	switch value := object[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	}
	return 0
}
