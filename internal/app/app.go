package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Dylan-Liew/closeview/internal/exporter"
	"github.com/Dylan-Liew/closeview/internal/importer"
	"github.com/Dylan-Liew/closeview/internal/native"
	"github.com/Dylan-Liew/closeview/internal/selector"
	"github.com/Dylan-Liew/closeview/internal/server"
	"github.com/Dylan-Liew/closeview/internal/store"
)

const defaultPort = "3434"

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}

	switch args[0] {
	case "import":
		return runImport(ctx, args[1:])
	case "list":
		return runList(ctx, args[1:])
	case "select":
		return runSelect(ctx, args[1:])
	case "show":
		return runShow(ctx, args[1:])
	case "export":
		return runExport(ctx, args[1:])
	case "open":
		return runOpen(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	dbPath := fs.String("db", "", "database path")
	source := fs.String("source", "auto", "source harness")
	title := fs.String("title", "", "session title override")
	project := fs.String("project", "", "project path")
	force := fs.Bool("force", false, "import even if the source hash already exists")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	inputPath := ""
	if fs.NArg() > 0 {
		inputPath = fs.Arg(0)
	} else if *source == "opencode" || *source == "opencode-db" {
		defaultPath := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "opencode.db")
		if _, err := os.Stat(defaultPath); err == nil {
			inputPath = defaultPath
		}
	}
	if inputPath == "" {
		selected, err := selector.SelectPath("Select a log file or directory to import")
		if err != nil {
			return err
		}
		inputPath = selected
	}

	db, err := openDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := importer.ImportPath(ctx, db, importer.Options{
		Path:        inputPath,
		Source:      *source,
		Title:       *title,
		ProjectPath: *project,
		Force:       *force,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Imported %d session(s), skipped %d from %s\n", result.SessionCount, result.SkippedCount, inputPath)
	for _, warning := range result.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	return nil
}

func runList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	dbPath := fs.String("db", "", "database path")
	query := fs.String("search", "", "search query")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	db, err := openDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	sessions, err := db.ListSessions(ctx, *query)
	if err != nil {
		return err
	}
	printSessions(sessions)
	return nil
}

func runSelect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	dbPath := fs.String("db", "", "database path")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	db, err := openDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	session, err := selectSession(ctx, db, "Select a session")
	if err != nil {
		return err
	}
	fmt.Println(session.ID)
	return nil
}

func runShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	dbPath := fs.String("db", "", "database path")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	db, err := openDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	sessionID := ""
	if fs.NArg() > 0 {
		sessionID = fs.Arg(0)
	} else {
		session, err := selectSession(ctx, db, "Show which session?")
		if err != nil {
			return err
		}
		sessionID = session.ID
	}

	session, messages, err := db.GetSessionWithMessages(ctx, sessionID)
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n\n", session.Title)
	fmt.Printf("id: %s\nsource: %s\nproject: %s\nmessages: %d\n\n", session.ID, session.Source, session.ProjectPath, session.MessageCount)
	for _, message := range messages {
		fmt.Printf("[%03d] %s\n%s\n\n", message.Sequence, message.Role, strings.TrimSpace(message.Content))
	}
	return nil
}

func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dbPath := fs.String("db", "", "database path")
	outPath := fs.String("out", "", "output path")
	format := fs.String("format", "html", "export format: html or json")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	db, err := openDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var sessionIDs []string
	if fs.NArg() > 0 {
		for _, arg := range fs.Args() {
			session, err := db.GetSession(ctx, arg)
			if err != nil {
				return err
			}
			sessionIDs = append(sessionIDs, session.ID)
		}
	} else {
		sessions, err := selectSessions(ctx, db, "Export which session(s)?")
		if err != nil {
			return err
		}
		for _, session := range sessions {
			sessionIDs = append(sessionIDs, session.ID)
		}
	}

	for i, sessionID := range sessionIDs {
		path := *outPath
		if len(sessionIDs) > 1 && path != "" {
			base := strings.TrimSuffix(path, filepath.Ext(path))
			ext := filepath.Ext(path)
			if ext == "" {
				ext = "." + *format
			}
			path = fmt.Sprintf("%s-%d%s", base, i+1, ext)
		}
		written, err := exporter.ExportSession(ctx, db, exporter.Options{SessionID: sessionID, Format: *format, OutPath: path})
		if err != nil {
			return err
		}
		fmt.Printf("Exported %s\n", written)
	}
	return nil
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "server host")
	port := fs.String("port", defaultPort, "server port")
	open := fs.Bool("open", false, "open the web viewer in a browser")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	sessions, err := native.NewDefault()
	if err != nil {
		return err
	}

	addr := *host + ":" + *port
	webURL := "http://" + addr
	fmt.Printf("CloseView listening at %s\n", webURL)
	warnIfPublicHost(*host)
	if *open {
		openSoon(webURL)
	}
	return server.Serve(ctx, sessions, addr)
}

func runOpen(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "server host")
	port := fs.String("port", defaultPort, "server port")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}

	sessions, err := native.NewDefault()
	if err != nil {
		return err
	}

	sessionID := ""
	if fs.NArg() > 0 {
		sessionID = fs.Arg(0)
	}

	addr := *host + ":" + *port
	webURL := "http://" + addr
	if sessionID != "" {
		webURL += "/?session=" + url.QueryEscape(sessionID)
	}
	fmt.Printf("Opening %s\n", webURL)
	warnIfPublicHost(*host)
	openSoon(webURL)
	return server.Serve(ctx, sessions, addr)
}

func openDB(path string) (*store.DB, error) {
	if path == "" {
		var err error
		path, err = defaultDBPath()
		if err != nil {
			return nil, err
		}
	}
	return store.Open(path)
}

func defaultDBPath() (string, error) {
	if env := os.Getenv("CLOSEVIEW_DB"); env != "" {
		return env, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "closeview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "closeview.db"), nil
}

func selectSession(ctx context.Context, db *store.DB, prompt string) (store.Session, error) {
	sessions, err := db.ListSessions(ctx, "")
	if err != nil {
		return store.Session{}, err
	}
	if len(sessions) == 0 {
		return store.Session{}, errors.New("no sessions imported yet")
	}
	return selector.SelectSession(prompt, sessions)
}

func selectSessions(ctx context.Context, db *store.DB, prompt string) ([]store.Session, error) {
	sessions, err := db.ListSessions(ctx, "")
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, errors.New("no sessions imported yet")
	}
	return selector.SelectSessions(prompt, sessions)
}

func printSessions(sessions []store.Session) {
	if len(sessions) == 0 {
		fmt.Println("No sessions found. Run `closeview import` first.")
		return
	}
	fmt.Printf("%-10s  %-12s  %-5s  %s\n", "ID", "SOURCE", "MSGS", "TITLE")
	for _, session := range sessions {
		id := session.ID
		if len(id) > 10 {
			id = id[:10]
		}
		fmt.Printf("%-10s  %-12s  %-5d  %s\n", id, session.Source, session.MessageCount, session.Title)
	}
}

func normalizeFlagArgs(args []string) []string {
	var flags []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, arg)
	}
	return append(flags, positional...)
}

func openSoon(targetURL string) {
	go func() {
		time.Sleep(350 * time.Millisecond)
		if err := openBrowser(targetURL); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not open browser: %v\n", err)
		}
	}()
}

func openBrowser(targetURL string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{targetURL}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", targetURL}
	default:
		command = "xdg-open"
		args = []string{targetURL}
	}
	return exec.Command(command, args...).Start()
}

func warnIfPublicHost(host string) {
	switch host {
	case "", "127.0.0.1", "localhost", "::1", "[::1]":
		return
	default:
		fmt.Fprintf(os.Stderr, "warning: CloseView has no authentication; binding to %s may expose local agent sessions and deletion controls\n", host)
	}
}

func usage() {
	fmt.Println(`CloseView - local coding-agent session viewer

Usage:
  closeview import [path] [--source auto] [--title title]
  closeview list [--search query]
  closeview select
  closeview show [session_id]
  closeview export [session_id...] [--format html|json] [--out path]
  closeview open [native_session_id]
  closeview serve [--open] [--host 127.0.0.1] [--port 3434]

The web viewer discovers OpenCode, Codex, and Claude sessions directly. Legacy import, list, show, and export commands use CloseView's import database.`)
}
