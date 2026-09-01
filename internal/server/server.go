package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Dylan-Liew/closeview/internal/native"
)

//go:embed web
var webFiles embed.FS

type api struct {
	sessions *native.Manager
}

func Serve(ctx context.Context, sessions *native.Manager, addr string) error {
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		return err
	}
	handler := &api{sessions: sessions}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", handler.listSessions)
	mux.HandleFunc("GET /api/sessions/", handler.getSession)
	mux.HandleFunc("DELETE /api/sessions/", handler.deleteSession)
	mux.Handle("/", securityHeaders(http.FileServer(http.FS(webRoot))))

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (a *api) listSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.sessions.List(r.Context(), r.URL.Query().Get("search"), r.URL.Query().Get("source")))
}

func (a *api) getSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" {
		writeError(w, fmt.Errorf("session id is required"), http.StatusBadRequest)
		return
	}
	detail, err := a.sessions.Get(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, native.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, err, status)
		return
	}
	writeJSON(w, detail)
}

func (a *api) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" {
		writeError(w, fmt.Errorf("session id is required"), http.StatusBadRequest)
		return
	}
	if r.Header.Get("X-CloseView-Confirm") != "delete-session" {
		writeError(w, fmt.Errorf("session deletion requires explicit confirmation"), http.StatusPreconditionRequired)
		return
	}
	if err := a.sessions.Delete(r.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, native.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, err, status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("cache-control", "no-store")
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-security-policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("referrer-policy", "no-referrer")
		w.Header().Set("x-content-type-options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
