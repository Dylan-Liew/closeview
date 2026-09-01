package common

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/Dylan-Liew/closeview/internal/store"
)

var codeFenceRE = regexp.MustCompile("(?s)```([^\n`]*)\n(.*?)```")

func SHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func EnrichMessages(messages []store.NewMessage) []store.NewMessage {
	for i := range messages {
		messages[i].CodeBlocks = ExtractCodeBlocks(messages[i].Content)
		if messages[i].Role == "tool" && len(messages[i].ToolCalls) == 0 {
			name := InferToolName(messages[i].Content)
			messages[i].ToolCalls = append(messages[i].ToolCalls, store.NewToolCall{
				Name:   name,
				Kind:   InferToolKind(name, messages[i].Content),
				Status: "unknown",
				Input:  FirstLine(messages[i].Content),
				Output: messages[i].Content,
			})
		}
		if strings.Contains(strings.ToLower(messages[i].Content), "tool call") && len(messages[i].ToolCalls) == 0 {
			messages[i].ToolCalls = append(messages[i].ToolCalls, store.NewToolCall{Name: "tool", Kind: "unknown", Status: "unknown", Input: FirstLine(messages[i].Content), Output: messages[i].Content})
		}
	}
	return messages
}

func ExtractCodeBlocks(content string) []store.NewCodeBlock {
	matches := codeFenceRE.FindAllStringSubmatch(content, -1)
	blocks := make([]store.NewCodeBlock, 0, len(matches))
	for _, match := range matches {
		blocks = append(blocks, store.NewCodeBlock{Language: strings.TrimSpace(match[1]), Content: strings.TrimSuffix(match[2], "\n")})
	}
	return blocks
}

func InferToolName(content string) string {
	line := strings.ToLower(FirstLine(content))
	switch {
	case strings.HasPrefix(line, "go "):
		return "go"
	case strings.HasPrefix(line, "git "):
		return "git"
	case strings.HasPrefix(line, "npm ") || strings.HasPrefix(line, "pnpm ") || strings.HasPrefix(line, "yarn "):
		return "package-manager"
	case strings.HasPrefix(line, "curl "):
		return "curl"
	default:
		return "tool"
	}
}

func InferToolKind(name string, input string) string {
	text := strings.ToLower(name + " " + input)
	trimmed := strings.TrimSpace(text)
	switch {
	case strings.Contains(text, "bash") || strings.Contains(text, "shell") || strings.HasPrefix(trimmed, "go ") || strings.HasPrefix(trimmed, "git ") || strings.Contains(text, "npm "):
		return "shell"
	case strings.Contains(text, "read"):
		return "file_read"
	case strings.Contains(text, "write") || strings.Contains(text, "edit"):
		return "file_write"
	case strings.Contains(text, "search") || strings.Contains(text, "grep"):
		return "search"
	default:
		return "unknown"
	}
}

func FirstLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human":
		return "user"
	case "assistant", "ai", "agent":
		return "assistant"
	case "system":
		return "system"
	case "tool", "function":
		return "tool"
	default:
		return "unknown"
	}
}
