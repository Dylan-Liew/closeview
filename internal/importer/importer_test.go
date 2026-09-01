package importer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dylan-Liew/closeview/internal/store"
)

func TestImportSkipsDuplicateUnlessForced(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.md")
	if err := os.WriteFile(logPath, []byte("# Session\n\n## User\n\nHello"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "closeview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first, err := ImportPath(context.Background(), db, Options{Path: logPath})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportPath(context.Background(), db, Options{Path: logPath})
	if err != nil {
		t.Fatal(err)
	}
	forced, err := ImportPath(context.Background(), db, Options{Path: logPath, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	forcedAgain, err := ImportPath(context.Background(), db, Options{Path: logPath, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionCount != 1 || second.SkippedCount != 1 || forced.SessionCount != 1 || forcedAgain.SessionCount != 1 {
		t.Fatalf("unexpected counts: first=%+v second=%+v forced=%+v forcedAgain=%+v", first, second, forced, forcedAgain)
	}
	sessions, err := db.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions after two forced imports, got %d", len(sessions))
	}
}

func TestImportDirectorySkipsSQLiteFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.md"), []byte("## User\n\nHello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.db"), []byte("not a log"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "closeview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := ImportPath(context.Background(), db, Options{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionCount != 1 {
		t.Fatalf("expected one markdown import, got %+v", result)
	}
}

func TestOpenCodeImportReplacesExistingSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.db")
	if err := writeOpenCodeDB(path, "first"); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "closeview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := ImportPath(context.Background(), db, Options{Path: path, Source: "opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeDB(path, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportPath(context.Background(), db, Options{Path: path, Source: "opencode"}); err != nil {
		t.Fatal(err)
	}

	sessions, err := db.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected replacement instead of duplicate, got %d sessions", len(sessions))
	}
	detail, err := db.GetSessionDetail(context.Background(), sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Messages) != 1 || !strings.Contains(detail.Messages[0].Content, "second") {
		t.Fatalf("expected refreshed message content, got %+v", detail.Messages)
	}
	if !strings.HasPrefix(detail.Messages[0].CreatedAt, "2023-") {
		t.Fatalf("expected source message timestamp, got %q", detail.Messages[0].CreatedAt)
	}
}

func writeOpenCodeDB(path string, text string) error {
	_ = os.Remove(path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	statements := []string{
		`create table session (id text primary key, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table message (id text primary key, session_id text, data text, time_created integer)`,
		`create table part (id text primary key, session_id text, message_id text, data text, time_created integer)`,
		`insert into session (id, title, directory, agent, model, time_created, time_updated) values ('ses1', 'OpenCode test', '/tmp/project', 'build', '{"id":"gpt-test","providerID":"openai"}', 1700000000000, 1700000001000)`,
		`insert into message (id, session_id, data, time_created) values ('msg1', 'ses1', '{"role":"user","modelID":"gpt-test","providerID":"openai","tokens":{"input":1,"output":2,"reasoning":3,"cache":{"read":4,"write":5}}}', 1700000000000)`,
		`insert into part (id, session_id, message_id, data, time_created) values ('part1', 'ses1', 'msg1', '{"type":"text","text":"` + text + `"}', 1700000000000)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
