package native

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

const codexAppServerHelperEnv = "GO_WANT_CLOSEVIEW_CODEX_APP_SERVER_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(codexAppServerHelperEnv) == "1" {
		os.Exit(runCodexAppServerHelper())
	}
	os.Exit(m.Run())
}

func TestCodexAppServerProcessClient(t *testing.T) {
	home := t.TempDir()
	t.Setenv(codexAppServerHelperEnv, "1")
	client := newCodexRPCClient(os.Args[0], home)
	defer client.Close()

	var page struct {
		Data      []codexThread `json:"data"`
		CodexHome string        `json:"codexHome"`
	}
	if err := client.Call(context.Background(), "thread/list", map[string]any{
		"limit":          1,
		"useStateDbOnly": true,
	}, &page); err != nil {
		t.Fatal(err)
	}
	if page.CodexHome != home {
		t.Fatalf("app-server did not receive CODEX_HOME: got %q want %q", page.CodexHome, home)
	}
	if len(page.Data) != 1 || page.Data[0].ID != "thread-helper" {
		t.Fatalf("unexpected helper response: %+v", page)
	}
}

func runCodexAppServerHelper() int {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := decoder.Decode(&request); err != nil {
			return 0
		}
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result["userAgent"] = "closeview-test-helper"
		case "thread/list":
			result["data"] = []map[string]any{{"id": "thread-helper", "name": "Helper thread"}}
			result["nextCursor"] = nil
			result["codexHome"] = os.Getenv("CODEX_HOME")
		default:
			result = map[string]any{}
		}
		if err := encoder.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  result,
		}); err != nil {
			return 1
		}
	}
}
