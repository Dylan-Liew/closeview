package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexCumulativeUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	// Repeated cumulative snapshots must not be added together. A later null
	// info event (such as a rate-limit update) must not discard recorded usage.
	data := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":50,"reasoning_output_tokens":10}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"output_tokens":40,"cached_input_tokens":120,"reasoning_output_tokens":15}}}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"output_tokens":40,"cached_input_tokens":120,"reasoning_output_tokens":15}}}}
{"type":"event_msg","payload":{"type":"token_count","info":null}}
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseCodexRollout(path, true)
	if err != nil {
		t.Fatal(err)
	}
	usage := parsed.detail.Usage
	if usage == nil || *usage != (TokenUsage{Input: 200, Output: 40, Cached: 120, Reasoning: 15}) {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	listed, err := parseCodexRollout(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if listed.detail.Usage != nil {
		t.Fatal("catalog should not load usage")
	}
}

func TestCodexMissingUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseCodexRollout(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.detail.Usage != nil {
		t.Fatal("missing usage should not appear as zero spend")
	}
}
