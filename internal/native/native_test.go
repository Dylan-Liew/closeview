package native

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCodeListGetDelete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`pragma foreign_keys = on`,
		`create table session (id text primary key, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table message (id text primary key, session_id text references session(id), data text, time_created integer)`,
		`create table part (id text primary key, session_id text references session(id), message_id text references message(id), data text, time_created integer)`,
		`insert into session values ('ses-test', 'Fix login', '/tmp/project', 'build', '{"id":"claude-test","providerID":"anthropic"}', 1700000000000, 1700000001000)`,
		`insert into message values ('msg-test', 'ses-test', '{"role":"user"}', 1700000000000)`,
		`insert into part values ('part-test', 'ses-test', 'msg-test', '{"type":"text","text":"Hello OpenCode"}', 1700000000000)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := NewOpenCode(path)
	sessions, err := adapter.List(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list: sessions=%+v err=%v", sessions, err)
	}
	if sessions[0].ThreadID != "ses-test" || sessions[0].MessageCount != 1 {
		t.Fatalf("unexpected summary: %+v", sessions[0])
	}
	detail, err := adapter.Get(context.Background(), "ses-test")
	if err != nil || len(detail.Messages) != 1 || detail.Messages[0].Content != "Hello OpenCode" {
		t.Fatalf("get: detail=%+v err=%v", detail, err)
	}
	if err := adapter.Delete(context.Background(), "ses-test"); err != nil {
		t.Fatal(err)
	}
	sessions, err = adapter.List(context.Background())
	if err != nil || len(sessions) != 0 {
		t.Fatalf("session remained after delete: %+v err=%v", sessions, err)
	}
}

func TestCodexListGetDelete(t *testing.T) {
	home := t.TempDir()
	rolloutDir := filepath.Join(home, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(rolloutDir, "rollout-test.jsonl")
	writeFixture(t, rollout, strings.Join([]string{
		`{"timestamp":"2026-01-02T03:04:05Z","type":"session_meta","payload":{"id":"thread-test","timestamp":"2026-01-02T03:04:05Z","cwd":"/tmp/codex","originator":"Codex"}}`,
		`{"timestamp":"2026-01-02T03:04:06Z","type":"turn_context","payload":{"model":"gpt-test"}}`,
		`{"timestamp":"2026-01-02T03:04:07Z","type":"response_item","payload":{"id":"u1","type":"message","role":"user","content":[{"type":"input_text","text":"Build the viewer"}]}}`,
		`{"timestamp":"2026-01-02T03:04:08Z","type":"response_item","payload":{"id":"a1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Working on it"}]}}`,
		`{"timestamp":"2026-01-02T03:04:09Z","type":"response_item","payload":{"id":"c1","type":"custom_tool_call","call_id":"call-1","name":"exec","input":"go test ./..."}}`,
		`{"timestamp":"2026-01-02T03:04:10Z","type":"response_item","payload":{"id":"o1","type":"custom_tool_call_output","call_id":"call-1","output":"ok"}}`,
	}, "\n")+"\n")
	writeFixture(t, filepath.Join(home, "session_index.jsonl"), `{"id":"thread-test","thread_name":"CloseView revamp","updated_at":"2026-01-02T03:04:11Z"}`+"\n")
	writeFixture(t, filepath.Join(home, "history.jsonl"), `{"session_id":"thread-test","text":"Build the viewer","ts":1}`+"\n")

	adapter := NewCodex(home)
	sessions, err := adapter.List(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].Title != "CloseView revamp" {
		t.Fatalf("list: sessions=%+v err=%v", sessions, err)
	}
	detail, err := adapter.Get(context.Background(), sessions[0].NativeID)
	if err != nil || len(detail.Messages) != 2 || len(detail.ToolCalls) != 1 || detail.ToolCalls[0].Output != "ok" {
		t.Fatalf("get: detail=%+v err=%v", detail, err)
	}
	if err := adapter.Delete(context.Background(), sessions[0].NativeID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rollout); !os.IsNotExist(err) {
		t.Fatalf("rollout still exists: %v", err)
	}
	assertFileDoesNotContain(t, filepath.Join(home, "session_index.jsonl"), "thread-test")
	assertFileDoesNotContain(t, filepath.Join(home, "history.jsonl"), "thread-test")
}

func TestClaudeListGetDelete(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "projects", "-tmp-project")
	if err := os.MkdirAll(filepath.Join(projectDir, "claude-test", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(projectDir, "claude-test.jsonl")
	writeFixture(t, transcript, strings.Join([]string{
		`{"type":"user","uuid":"u1","sessionId":"claude-test","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:05Z","message":{"role":"user","content":"Inspect the code"}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"claude-test","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:06Z","message":{"role":"assistant","model":"claude-test-model","content":[{"type":"text","text":"I found it"},{"type":"tool_use","id":"tool-1","name":"Read","input":{"file_path":"main.go"}}],"usage":{"input_tokens":10,"output_tokens":4}}}`,
		`{"type":"user","uuid":"u2","sessionId":"claude-test","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:07Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"package main"}]}}`,
	}, "\n")+"\n")
	writeFixture(t, filepath.Join(home, "history.jsonl"), `{"sessionId":"claude-test","display":"Inspect the code"}`+"\n")

	adapter := NewClaude(home)
	sessions, err := adapter.List(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].MessageCount != 2 {
		t.Fatalf("list: sessions=%+v err=%v", sessions, err)
	}
	detail, err := adapter.Get(context.Background(), sessions[0].NativeID)
	if err != nil || len(detail.Messages) != 2 || len(detail.ToolCalls) != 1 || !strings.Contains(detail.ToolCalls[0].Output, "package main") {
		t.Fatalf("get: detail=%+v err=%v", detail, err)
	}
	if err := adapter.Delete(context.Background(), sessions[0].NativeID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "claude-test")); !os.IsNotExist(err) {
		t.Fatalf("session artifact directory still exists: %v", err)
	}
	assertFileDoesNotContain(t, filepath.Join(home, "history.jsonl"), "claude-test")
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileDoesNotContain(t *testing.T, path, value string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), value) {
		t.Fatalf("%s still contains %q", path, value)
	}
}
