package native

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrNotFound = errors.New("session not found")

type Session struct {
	ID             string `json:"id"`
	NativeID       string `json:"nativeId"`
	ThreadID       string `json:"threadId"`
	ParentID       string `json:"parentId,omitempty"`
	ParentThreadID string `json:"parentThreadId,omitempty"`
	IsSubsession   bool   `json:"isSubsession"`
	ChildCount     int    `json:"childCount"`
	Source         string `json:"source"`
	Title          string `json:"title"`
	ProjectPath    string `json:"projectPath"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Agent          string `json:"agent"`
	Origin         string `json:"origin"`
	MessageCount   int    `json:"messageCount"`
}

type Message struct {
	ID               string  `json:"id"`
	Sequence         int     `json:"sequence"`
	Role             string  `json:"role"`
	Content          string  `json:"content"`
	CreatedAt        string  `json:"createdAt"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Finish           string  `json:"finish"`
	Cost             float64 `json:"cost"`
	TokensInput      int     `json:"tokensInput"`
	TokensOutput     int     `json:"tokensOutput"`
	TokensReasoning  int     `json:"tokensReasoning"`
	TokensCacheRead  int     `json:"tokensCacheRead"`
	TokensCacheWrite int     `json:"tokensCacheWrite"`
}

type ToolCall struct {
	ID        string `json:"id"`
	MessageID string `json:"messageId"`
	Sequence  int    `json:"sequence"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Input     string `json:"input"`
	Output    string `json:"output"`
}

type Detail struct {
	Session   Session     `json:"session"`
	Messages  []Message   `json:"messages"`
	ToolCalls []ToolCall  `json:"toolCalls"`
	Warnings  []string    `json:"warnings"`
	Usage     *TokenUsage `json:"usage,omitempty"`
}

// Codex reports cumulative totals; cached input and reasoning are subsets.
type TokenUsage struct {
	Input     int `json:"input_tokens"`
	Output    int `json:"output_tokens"`
	Cached    int `json:"cached_input_tokens"`
	Reasoning int `json:"reasoning_output_tokens"`
}

type SourceStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Count     int    `json:"count"`
	Error     string `json:"error,omitempty"`
}

type Catalog struct {
	Sessions []Session      `json:"sessions"`
	Sources  []SourceStatus `json:"sources"`
}

type Adapter interface {
	Name() string
	List(context.Context) ([]Session, error)
	Get(context.Context, string) (Detail, error)
	Delete(context.Context, string) error
}

type Manager struct {
	adapters map[string]Adapter
	order    []string
}

func New(adapters ...Adapter) *Manager {
	m := &Manager{adapters: make(map[string]Adapter)}
	for _, adapter := range adapters {
		name := adapter.Name()
		m.adapters[name] = adapter
		m.order = append(m.order, name)
	}
	return m
}

func NewDefault() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	opencodePath := os.Getenv("CLOSEVIEW_OPENCODE_DB")
	if opencodePath == "" {
		opencodePath = filepath.Join(dataHome, "opencode", "opencode.db")
	}
	codexHome := os.Getenv("CLOSEVIEW_CODEX_HOME")
	if codexHome == "" {
		codexHome = os.Getenv("CODEX_HOME")
	}
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	claudeHome := os.Getenv("CLOSEVIEW_CLAUDE_HOME")
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	return New(
		NewOpenCode(opencodePath),
		NewCodex(codexHome),
		NewClaude(claudeHome),
	), nil
}

func (m *Manager) List(ctx context.Context, query, source string) Catalog {
	catalog := Catalog{Sessions: []Session{}, Sources: []SourceStatus{}}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, name := range m.order {
		if source != "" && source != "all" && source != name {
			continue
		}
		sessions, err := m.adapters[name].List(ctx)
		status := SourceStatus{Name: name, Available: err == nil, Count: len(sessions)}
		if err != nil {
			status.Error = err.Error()
		}
		catalog.Sources = append(catalog.Sources, status)
		for _, session := range sessions {
			session.Source = name
			session.ID = EncodeID(name, session.NativeID)
			session.IsSubsession = session.ParentThreadID != ""
			catalog.Sessions = append(catalog.Sessions, session)
		}
	}
	linkSessionRelationships(catalog.Sessions)
	if query != "" {
		matched := catalog.Sessions[:0]
		for _, session := range catalog.Sessions {
			if sessionMatches(session, query) {
				matched = append(matched, session)
			}
		}
		catalog.Sessions = matched
	}
	sort.SliceStable(catalog.Sessions, func(i, j int) bool {
		left := catalog.Sessions[i].UpdatedAt
		if left == "" {
			left = catalog.Sessions[i].CreatedAt
		}
		right := catalog.Sessions[j].UpdatedAt
		if right == "" {
			right = catalog.Sessions[j].CreatedAt
		}
		if left == right {
			return catalog.Sessions[i].ID > catalog.Sessions[j].ID
		}
		return left > right
	})
	return catalog
}

func (m *Manager) Get(ctx context.Context, id string) (Detail, error) {
	adapter, nativeID, err := m.resolve(id)
	if err != nil {
		return Detail{}, err
	}
	detail, err := adapter.Get(ctx, nativeID)
	if err != nil {
		return Detail{}, err
	}
	detail.Session.Source = adapter.Name()
	detail.Session.NativeID = nativeID
	detail.Session.ID = EncodeID(adapter.Name(), nativeID)
	detail.Session.IsSubsession = detail.Session.ParentThreadID != ""
	if sessions, listErr := adapter.List(ctx); listErr == nil {
		for _, session := range sessions {
			if session.ThreadID == detail.Session.ParentThreadID {
				detail.Session.ParentID = EncodeID(adapter.Name(), session.NativeID)
			}
			if session.ParentThreadID == detail.Session.ThreadID {
				detail.Session.ChildCount++
			}
		}
	}
	return detail, nil
}

func linkSessionRelationships(sessions []Session) {
	byThread := make(map[string]string, len(sessions))
	for _, session := range sessions {
		if session.ThreadID != "" {
			byThread[session.Source+"\x00"+session.ThreadID] = session.ID
		}
	}
	byID := make(map[string]int, len(sessions))
	for index := range sessions {
		byID[sessions[index].ID] = index
		if sessions[index].ParentThreadID != "" {
			sessions[index].ParentID = byThread[sessions[index].Source+"\x00"+sessions[index].ParentThreadID]
		}
	}
	for _, session := range sessions {
		if index, ok := byID[session.ParentID]; ok {
			sessions[index].ChildCount++
		}
	}
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	adapter, nativeID, err := m.resolve(id)
	if err != nil {
		return err
	}
	return adapter.Delete(ctx, nativeID)
}

func (m *Manager) resolve(id string) (Adapter, string, error) {
	source, nativeID, err := DecodeID(id)
	if err != nil {
		return nil, "", err
	}
	adapter, ok := m.adapters[source]
	if !ok {
		return nil, "", fmt.Errorf("unknown session source %q", source)
	}
	return adapter, nativeID, nil
}

func EncodeID(source, nativeID string) string {
	return source + "." + base64.RawURLEncoding.EncodeToString([]byte(nativeID))
}

func DecodeID(id string) (string, string, error) {
	source, encoded, ok := strings.Cut(id, ".")
	if !ok || source == "" || encoded == "" {
		return "", "", fmt.Errorf("invalid session id")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return "", "", fmt.Errorf("invalid session id")
	}
	return source, string(decoded), nil
}

func sessionMatches(session Session, query string) bool {
	text := strings.ToLower(strings.Join([]string{
		session.Title, session.Source, session.NativeID, session.ThreadID, session.ProjectPath,
		session.Provider, session.Model, session.Agent, session.Origin,
	}, "\n"))
	return strings.Contains(text, query)
}

func titleFallback(title, firstPrompt, projectPath, nativeID string) string {
	for _, value := range []string{title, firstPrompt, filepath.Base(projectPath), nativeID} {
		value = strings.TrimSpace(value)
		if value != "" && value != "." {
			if len(value) > 100 {
				return value[:100] + "…"
			}
			return value
		}
	}
	return "Untitled session"
}
