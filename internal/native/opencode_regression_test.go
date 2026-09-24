package native

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func openCodeV2Fixture(t *testing.T) (*sql.DB, Adapter) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`create table session_v2 (id text primary key, parent_id text, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table session_message (id text primary key, session_id text, type text, seq integer, time_created integer, data text)`,
		`create table session (id text primary key, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table message (id text primary key, session_id text, time_created integer, data text)`,
		`create table part (id text primary key, session_id text, message_id text, time_created integer, data text)`,
		`insert into session_v2 (id) values ('good'), ('empty'), ('broken')`,
		`insert into session_message values ('good-message', 'good', 'user', 1, 1, '{"text":"hello"}'), ('bad-message', 'broken', 'user', 1, 1, 'invalid-json')`,
		`insert into session (id) values ('good'), ('legacy-only')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return db, NewOpenCode(path)
}

func TestOpenCodeWarningsAreSessionScoped(t *testing.T) {
	_, adapter := openCodeV2Fixture(t)
	for _, id := range []string{"good", "empty"} {
		detail, err := adapter.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Warnings) != 0 {
			t.Fatalf("%s inherited warnings: %v", id, detail.Warnings)
		}
		if id == "good" && (len(detail.Messages) != 1 || detail.Messages[0].Content != "hello") {
			t.Fatalf("unexpected detail: %+v", detail)
		}
	}
	detail, err := adapter.Get(context.Background(), "broken")
	if err != nil || len(detail.Warnings) != 1 || !strings.Contains(detail.Warnings[0], "broken/bad-message") {
		t.Fatalf("missing session-specific warning: %v, %v", detail.Warnings, err)
	}
}

func TestOpenCodeV2DoesNotResurrectLegacySessions(t *testing.T) {
	db, adapter := openCodeV2Fixture(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/api/session/good" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if _, err := db.Exec(`delete from session_message where session_id='good'; delete from session_v2 where id='good'`); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("CLOSEVIEW_OPENCODE_URL", server.URL)
	t.Setenv("CLOSEVIEW_OPENCODE_PASSWORD_FILE", "")
	t.Setenv("CLOSEVIEW_OPENCODE_PASSWORD", "")
	if err := adapter.Delete(context.Background(), "good"); err != nil {
		t.Fatal(err)
	}
	sessions, err := adapter.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("legacy sessions leaked into catalog: %+v", sessions)
	}
	for _, id := range []string{"good", "legacy-only"} {
		if _, err := adapter.Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%q): %v", id, err)
		}
		if err := adapter.Delete(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(%q): %v", id, err)
		}
	}
	if calls != 1 {
		t.Fatalf("unexpected service calls: %d", calls)
	}
	var count int
	if err := db.QueryRow(`select count(*) from session`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("legacy history was modified: %d %v", count, err)
	}
}

func TestOpenCodeV2RejectsUnconfirmedDeletion(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			_, adapter := openCodeV2Fixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			t.Setenv("CLOSEVIEW_OPENCODE_URL", server.URL)
			t.Setenv("CLOSEVIEW_OPENCODE_PASSWORD_FILE", "")
			t.Setenv("CLOSEVIEW_OPENCODE_PASSWORD", "")
			err := adapter.Delete(context.Background(), "good")
			if err == nil {
				t.Fatal("reported success while session remained")
			}
			if status == http.StatusNoContent && !strings.Contains(err.Error(), "session remains") {
				t.Fatal(err)
			}
			if _, err := adapter.Get(context.Background(), "good"); err != nil {
				t.Fatalf("session was changed on failure: %v", err)
			}
		})
	}
}
