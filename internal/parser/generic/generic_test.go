package generic

import (
	"testing"

	"github.com/Dylan-Liew/closeview/internal/parser/common"
	"github.com/Dylan-Liew/closeview/internal/store"
)

func TestParseMarkdownExtractsCodeAndToolCall(t *testing.T) {
	content := "# Parser Work\n\n" +
		"## User\n\n" +
		"Import this.\n\n" +
		"## Assistant\n\n" +
		"Here is code:\n\n" +
		"```go\n" +
		"fmt.Println(\"ok\")\n" +
		"```\n\n" +
		"## Tool\n\n" +
		"go test ./...\n"

	messages, warnings := parseMessages("session.md", content)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	messages = common.EnrichMessages(messages)

	var codeBlocks []store.NewCodeBlock
	var toolCalls []store.NewToolCall
	for _, message := range messages {
		codeBlocks = append(codeBlocks, message.CodeBlocks...)
		toolCalls = append(toolCalls, message.ToolCalls...)
	}
	if len(codeBlocks) != 1 {
		t.Fatalf("expected one code block, got %d", len(codeBlocks))
	}
	if codeBlocks[0].Language != "go" {
		t.Fatalf("expected go code block, got %q", codeBlocks[0].Language)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Kind != "shell" {
		t.Fatalf("expected shell tool call, got %q", toolCalls[0].Kind)
	}
}
