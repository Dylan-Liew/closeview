package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dylan-Liew/closeview/internal/store"
)

type Options struct {
	SessionID string
	Format    string
	OutPath   string
}

type Bundle struct {
	Session     store.Session      `json:"session"`
	Messages    []store.Message    `json:"messages"`
	ToolCalls   []store.ToolCall   `json:"toolCalls"`
	CodeBlocks  []store.CodeBlock  `json:"codeBlocks"`
	ParseEvents []store.ParseEvent `json:"parseEvents"`
}

type Rendered struct {
	Filename    string
	ContentType string
	Content     []byte
}

func ExportSession(ctx context.Context, db *store.DB, opts Options) (string, error) {
	if opts.SessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	rendered, err := RenderSession(ctx, db, opts.SessionID, opts.Format)
	if err != nil {
		return "", err
	}
	outPath := opts.OutPath
	if outPath == "" {
		outPath = rendered.Filename
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		return "", err
	}
	return outPath, os.WriteFile(outPath, rendered.Content, 0o644)
}

func RenderSession(ctx context.Context, db *store.DB, sessionID string, format string) (Rendered, error) {
	if sessionID == "" {
		return Rendered{}, fmt.Errorf("session id is required")
	}
	detail, err := db.GetSessionDetail(ctx, sessionID)
	if err != nil {
		return Rendered{}, err
	}

	format = strings.ToLower(format)
	if format == "" {
		format = "html"
	}
	bundle := Bundle{Session: detail.Session, Messages: detail.Messages, ToolCalls: detail.ToolCalls, CodeBlocks: detail.CodeBlocks, ParseEvents: detail.ParseEvents}

	switch format {
	case "json":
		content, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return Rendered{}, err
		}
		return Rendered{Filename: defaultOutPath(detail.Session, format), ContentType: "application/json", Content: content}, nil
	case "html":
		var content bytes.Buffer
		if err := htmlTemplate.Execute(&content, bundle); err != nil {
			return Rendered{}, err
		}
		return Rendered{Filename: defaultOutPath(detail.Session, format), ContentType: "text/html; charset=utf-8", Content: content.Bytes()}, nil
	default:
		return Rendered{}, fmt.Errorf("unsupported export format %q", format)
	}
}

func defaultOutPath(session store.Session, format string) string {
	name := strings.ToLower(session.Title)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, name)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if len(name) > 80 {
		name = strings.TrimRight(name[:80], "-")
	}
	if name == "" {
		name = session.ID
	}
	return name + "." + format
}

var htmlTemplate = template.Must(template.New("export").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Session.Title}} - CloseView</title>
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 0; background: #111318; color: #e8edf2; }
    main { max-width: 980px; margin: 0 auto; padding: 32px 20px; }
    .meta { color: #9aa8b6; margin-bottom: 24px; }
    .msg, .tool { border: 1px solid #2b3440; border-radius: 14px; padding: 16px; margin: 14px 0; background: #171b22; }
    .tool { border-color: #3d4f2b; }
    .role { color: #79c0ff; font-weight: 700; text-transform: uppercase; font-size: 12px; letter-spacing: .08em; }
    pre { white-space: pre-wrap; word-break: break-word; line-height: 1.5; }
  </style>
</head>
<body><main>
  <h1>{{.Session.Title}}</h1>
  <div class="meta">{{.Session.Source}} · {{.Session.MessageCount}} messages · exported by CloseView</div>
  {{if .ParseEvents}}<h2>Parse Warnings</h2>{{range .ParseEvents}}<section class="tool"><strong>{{.Severity}}:</strong> {{.Message}}</section>{{end}}{{end}}
  {{if .ToolCalls}}<h2>Tool Calls</h2>{{range .ToolCalls}}<section class="tool"><div class="role">{{.Name}} · {{.Kind}} · {{.Status}}</div><pre>{{.Input}}{{if .Output}}

{{.Output}}{{end}}</pre></section>{{end}}{{end}}
  {{range .Messages}}
    <section class="msg"><div class="role">{{.Role}} · #{{.Sequence}}</div><pre>{{.Content}}</pre></section>
  {{end}}
</main></body></html>`))
