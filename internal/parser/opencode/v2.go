package opencode

import (
	"encoding/json"
	"github.com/Dylan-Liew/closeview/internal/store"
	"strings"
)

func parseV2Message(message sourceMessage) (store.NewMessage, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(message.Data), &raw); err != nil {
		return store.NewMessage{}, err
	}
	role := message.Role
	if role != "user" && role != "assistant" {
		role = "system"
	}
	result := store.NewMessage{Role: role, CreatedAt: formatOpenCodeTime(message.Created)}
	var content []string
	if text := stringValue(raw, "text"); text != "" {
		content = append(content, text)
	}
	if parts, ok := raw["content"].([]any); ok {
		for _, part := range parts {
			encoded, err := json.Marshal(part)
			if err != nil {
				return result, err
			}
			text, call := parsePart(string(encoded))
			if text != "" {
				content = append(content, text)
			}
			if call.Name != "" {
				result.ToolCalls = append(result.ToolCalls, call)
			}
		}
	}
	if message.Role == "shell" {
		content = append(content, stringValue(raw, "command"))
		if output, ok := raw["output"].(map[string]any); ok {
			content = append(content, stringValue(output, "text", "stdout"))
		}
	}
	if message.Role == "compaction" {
		content = append(content, stringValue(raw, "summary"))
	}
	if len(content) == 0 && len(result.ToolCalls) == 0 {
		content = append(content, message.Role+": "+message.Data)
	}
	result.Content = strings.TrimSpace(strings.Join(content, "\n\n"))
	metadata := parseMessageMetadata(message.Data)
	result.Provider, result.Model, result.Finish, result.Cost = metadata.Provider, metadata.Model, metadata.Finish, metadata.Cost
	result.TokensInput, result.TokensOutput, result.TokensReasoning = metadata.TokensInput, metadata.TokensOutput, metadata.TokensReasoning
	result.TokensCacheRead, result.TokensCacheWrite = metadata.TokensCacheRead, metadata.TokensCacheWrite
	return result, nil
}
