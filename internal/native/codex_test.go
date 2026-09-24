package native

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type fakeCodexRPC struct {
	listCalls   []map[string]any
	deletedIDs  []string
	turnListErr bool
	closed      bool
}

func (f *fakeCodexRPC) Call(_ context.Context, method string, params, result any) error {
	switch method {
	case "thread/list":
		f.listCalls = append(f.listCalls, params.(map[string]any))
		cursor, _ := params.(map[string]any)["cursor"].(string)
		if cursor == "" {
			return assignCodexResult(result, map[string]any{
				"data": []map[string]any{
					{
						"id":             "thread-parent",
						"name":           "CloseView revamp",
						"preview":        "Build the viewer",
						"parentThreadId": nil,
						"modelProvider":  "openai",
						"model":          "gpt-test",
						"createdAt":      1767322,
						"updatedAt":      1767333,
						"cwd":            "/tmp/codex",
						"originator":     "Codex CLI",
					},
				},
				"nextCursor": "page-2",
			})
		}
		return assignCodexResult(result, map[string]any{
			"data": []map[string]any{
				{
					"id":             "thread-child",
					"name":           "Review the viewer",
					"parentThreadId": "thread-parent",
					"agentRole":      "reviewer",
					"modelProvider":  "openai",
					"createdAt":      1767344,
					"updatedAt":      1767355,
					"cwd":            "/tmp/codex",
					"originator":     "Codex CLI",
				},
			},
			"nextCursor": nil,
		})
	case "thread/read":
		threadID, _ := params.(map[string]any)["threadId"].(string)
		return assignCodexResult(result, map[string]any{
			"thread": map[string]any{
				"id":            threadID,
				"name":          "CloseView revamp",
				"modelProvider": "openai",
				"model":         "gpt-test",
				"createdAt":     1767322,
				"updatedAt":     1767333,
				"cwd":           "/tmp/codex",
				"originator":    "Codex CLI",
			},
		})
	case "thread/turns/list":
		if f.turnListErr {
			return &codexRPCError{Code: -32600, Message: "invalid paginated history lineage"}
		}
		return assignCodexResult(result, map[string]any{
			"data": []map[string]any{
				{"id": "turn-1", "startedAt": 1767322, "completedAt": 1767333, "status": "completed"},
			},
			"nextCursor": nil,
		})
	case "thread/items/list":
		return assignCodexResult(result, map[string]any{
			"data": []map[string]any{
				{"turnId": "turn-1", "item": map[string]any{"type": "userMessage", "id": "user-1", "content": []map[string]any{{"type": "text", "text": "Build the viewer"}}}},
				{"turnId": "turn-1", "item": map[string]any{"type": "agentMessage", "id": "agent-1", "text": "Working on it", "phase": "commentary"}},
				{"turnId": "turn-1", "item": map[string]any{"type": "reasoning", "id": "reasoning-1", "summary": []string{"Inspecting the app"}}},
				{"turnId": "turn-1", "item": map[string]any{"type": "commandExecution", "id": "command-1", "command": "go test ./...", "aggregatedOutput": "ok", "status": "completed"}},
			},
			"nextCursor": nil,
		})
	case "thread/delete":
		threadID, _ := params.(map[string]any)["threadId"].(string)
		f.deletedIDs = append(f.deletedIDs, threadID)
		return assignCodexResult(result, map[string]any{})
	default:
		return &codexRPCError{Code: -32601, Message: "method not found"}
	}
}

func (f *fakeCodexRPC) Close() error {
	f.closed = true
	return nil
}

func assignCodexResult(result, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

func TestCodexAppServerListRelationshipsAndDetail(t *testing.T) {
	client := &fakeCodexRPC{}
	adapter := newCodexAdapter(client)
	manager := New(adapter)

	catalog := manager.List(context.Background(), "", "codex")
	if len(catalog.Sessions) != 2 || len(catalog.Sources) != 1 || !catalog.Sources[0].Available {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	parent := catalogSession(t, catalog, "thread-parent")
	child := catalogSession(t, catalog, "thread-child")
	if parent.Title != "CloseView revamp" || child.ParentID != parent.ID || child.Agent != "reviewer" || parent.ChildCount != 1 {
		t.Fatalf("unexpected relationships: parent=%+v child=%+v", parent, child)
	}
	if len(client.listCalls) != 2 {
		t.Fatalf("expected two list pages, got %d", len(client.listCalls))
	}
	if useStateDB, ok := client.listCalls[0]["useStateDbOnly"].(bool); !ok || useStateDB {
		t.Fatalf("app-server listing must reconcile all Codex sessions: %+v", client.listCalls[0])
	}
	if kinds, ok := client.listCalls[0]["sourceKinds"].([]string); !ok || len(kinds) != len(codexSourceKinds) {
		t.Fatalf("unexpected source kinds: %+v", client.listCalls[0]["sourceKinds"])
	}
	if client.listCalls[1]["cursor"] != "page-2" {
		t.Fatalf("second page did not use cursor: %+v", client.listCalls[1])
	}

	detail, err := adapter.Get(context.Background(), "thread-parent")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.NativeID != "thread-parent" || detail.Session.MessageCount != 3 {
		t.Fatalf("unexpected detail session: %+v", detail.Session)
	}
	if len(detail.Messages) != 3 || detail.Messages[0].Content != "Build the viewer" || detail.Messages[1].Finish != "commentary" {
		t.Fatalf("unexpected messages: %+v", detail.Messages)
	}
	if len(detail.ToolCalls) != 1 || detail.ToolCalls[0].Name != "shell" || detail.ToolCalls[0].Output != "ok" || detail.ToolCalls[0].Status != "completed" {
		t.Fatalf("unexpected tool calls: %+v", detail.ToolCalls)
	}
	if detail.ToolCalls[0].MessageID != "agent-1" {
		t.Fatalf("tool call was not attached to assistant message: %+v", detail.ToolCalls[0])
	}
}

func TestCodexAppServerDetailContinuesWithoutTurnMetadata(t *testing.T) {
	client := &fakeCodexRPC{turnListErr: true}
	adapter := newCodexAdapter(client)
	detail, err := adapter.Get(context.Background(), "thread-parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Messages) != 3 || len(detail.ToolCalls) != 1 {
		t.Fatalf("item history was not loaded: %+v", detail)
	}
	if len(detail.Warnings) != 1 || !strings.Contains(detail.Warnings[0], "turn metadata") {
		t.Fatalf("missing turn metadata warning: %+v", detail.Warnings)
	}
}

func TestCodexAppServerDeleteAndClose(t *testing.T) {
	client := &fakeCodexRPC{}
	adapter := newCodexAdapter(client)
	if err := adapter.Delete(context.Background(), "thread-parent"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.deletedIDs, []string{"thread-parent"}) {
		t.Fatalf("unexpected delete calls: %+v", client.deletedIDs)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if !client.closed {
		t.Fatal("app-server client was not closed")
	}
}
