package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestListSessionsSearchesMetadataAndMessages(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "closeview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	alpha, err := db.InsertSession(context.Background(), NewSession{
		Source:      "test",
		Title:       "alpha-beta",
		ProjectPath: "/tmp/example",
		SourceHash:  "alpha",
		Messages:    []NewMessage{{Role: "user", Content: "hello searchable content"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertSession(context.Background(), NewSession{
		Source:     "test",
		Title:      "other",
		SourceHash: "other",
		Messages:   []NewMessage{{Role: "assistant", Content: "nothing relevant"}},
	}); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"hello", "alpha-beta", alpha.ID[:10]} {
		sessions, err := db.ListSessions(context.Background(), query)
		if err != nil {
			t.Fatalf("search %q failed: %v", query, err)
		}
		if len(sessions) != 1 || sessions[0].ID != alpha.ID {
			t.Fatalf("search %q returned %+v", query, sessions)
		}
	}
}
