package library

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

type MCPServer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	ConfigFile  string   `json:"configFile"`
	Transport   string   `json:"transport"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args"`
	URL         string   `json:"url,omitempty"`
	Environment []string `json:"environment"`
	Headers     []string `json:"headers"`
	Auth        string   `json:"auth,omitempty"`
	Disabled    bool     `json:"disabled"`
}

type MCPCatalog struct {
	Servers []MCPServer    `json:"servers"`
	Sources []SourceStatus `json:"sources"`
}

func (l *Library) ListMCP() MCPCatalog {
	catalog := MCPCatalog{Servers: []MCPServer{}, Sources: []SourceStatus{}}
	for _, config := range l.mcpConfigs {
		servers, err := parseMCPConfig(config)
		status := SourceStatus{Name: config.source, Available: err == nil, Count: len(servers)}
		if err != nil {
			status.Error = err.Error()
		}
		catalog.Sources = append(catalog.Sources, status)
		catalog.Servers = append(catalog.Servers, servers...)
	}
	sort.SliceStable(catalog.Servers, func(i, j int) bool {
		if catalog.Servers[i].Source != catalog.Servers[j].Source {
			return sourceIndex(catalog.Servers[i].Source) < sourceIndex(catalog.Servers[j].Source)
		}
		return catalog.Servers[i].Name < catalog.Servers[j].Name
	})
	return catalog
}

func parseMCPConfig(config mcpConfig) ([]MCPServer, error) {
	if _, err := os.Stat(config.path); err != nil {
		if os.IsNotExist(err) {
			return []MCPServer{}, nil
		}
		return nil, err
	}
	switch config.source {
	case "opencode":
		return parseOpenCodeMCP(config)
	case "codex":
		return parseCodexMCP(config)
	case "claude":
		return parseClaudeMCP(config)
	default:
		return nil, fmt.Errorf("unsupported MCP source %q", config.source)
	}
}

func parseOpenCodeMCP(config mcpConfig) ([]MCPServer, error) {
	var document struct {
		MCP struct {
			Servers map[string]struct {
				Type        string         `json:"type"`
				Command     []string       `json:"command"`
				URL         string         `json:"url"`
				Headers     map[string]any `json:"headers"`
				Environment map[string]any `json:"environment"`
				OAuth       bool           `json:"oauth"`
				Disabled    bool           `json:"disabled"`
			} `json:"servers"`
		} `json:"mcp"`
	}
	if err := decodeJSONFile(config.path, &document); err != nil {
		return nil, fmt.Errorf("parse OpenCode MCP config: %w", err)
	}

	servers := make([]MCPServer, 0, len(document.MCP.Servers))
	for name, value := range document.MCP.Servers {
		command, args := commandFromSlice(value.Command)
		server := MCPServer{
			ID:          encodeLibraryID(config.source, name),
			Name:        name,
			Source:      config.source,
			ConfigFile:  config.label,
			Transport:   transport(value.Type, command, value.URL),
			Command:     command,
			Args:        sanitizeArgs(args),
			URL:         safeURL(value.URL),
			Environment: sortedKeys(value.Environment),
			Headers:     sortedKeys(value.Headers),
			Disabled:    value.Disabled,
		}
		if value.OAuth {
			server.Auth = "OAuth"
		}
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers, nil
}

func parseClaudeMCP(config mcpConfig) ([]MCPServer, error) {
	var document struct {
		MCPServers map[string]struct {
			Type     string         `json:"type"`
			Command  string         `json:"command"`
			Args     []string       `json:"args"`
			URL      string         `json:"url"`
			Env      map[string]any `json:"env"`
			Headers  map[string]any `json:"headers"`
			Disabled bool           `json:"disabled"`
		} `json:"mcpServers"`
	}
	if err := decodeJSONFile(config.path, &document); err != nil {
		return nil, fmt.Errorf("parse Claude MCP config: %w", err)
	}

	servers := make([]MCPServer, 0, len(document.MCPServers))
	for name, value := range document.MCPServers {
		server := MCPServer{
			ID:          encodeLibraryID(config.source, name),
			Name:        name,
			Source:      config.source,
			ConfigFile:  config.label,
			Transport:   transport(value.Type, value.Command, value.URL),
			Command:     sanitizeArg(value.Command),
			Args:        sanitizeArgs(value.Args),
			URL:         safeURL(value.URL),
			Environment: sortedKeys(value.Env),
			Headers:     sortedKeys(value.Headers),
			Disabled:    value.Disabled,
		}
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers, nil
}

func parseCodexMCP(config mcpConfig) ([]MCPServer, error) {
	file, err := os.Open(config.path)
	if err != nil {
		return nil, fmt.Errorf("read Codex MCP config: %w", err)
	}
	defer file.Close()

	type codexServer struct {
		command  string
		args     []string
		url      string
		envKeys  []string
		disabled bool
	}
	servers := make(map[string]*codexServer)
	current := ""
	inEnvironment := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = ""
			inEnvironment = false
			section := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			parts := strings.Split(section, ".")
			if len(parts) < 2 || unquoteSection(parts[0]) != "mcp_servers" {
				continue
			}
			name := unquoteSection(parts[1])
			if name == "" {
				continue
			}
			if _, exists := servers[name]; !exists {
				servers[name] = &codexServer{}
			}
			current = name
			inEnvironment = len(parts) == 3 && unquoteSection(parts[2]) == "env"
			continue
		}
		if current == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		server := servers[current]
		if inEnvironment {
			if key != "" {
				server.envKeys = append(server.envKeys, key)
			}
			continue
		}
		switch key {
		case "command":
			server.command = unquoteTOMLString(value)
		case "url":
			server.url = unquoteTOMLString(value)
		case "args":
			server.args = parseTOMLStringSlice(value)
		case "enabled":
			if parsed, err := strconv.ParseBool(value); err == nil {
				server.disabled = !parsed
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse Codex MCP config: %w", err)
	}

	result := make([]MCPServer, 0, len(servers))
	for name, value := range servers {
		result = append(result, MCPServer{
			ID:          encodeLibraryID(config.source, name),
			Name:        name,
			Source:      config.source,
			ConfigFile:  config.label,
			Transport:   transport("", value.command, value.url),
			Command:     sanitizeArg(value.command),
			Args:        sanitizeArgs(value.args),
			URL:         safeURL(value.url),
			Environment: sortedStringSlice(value.envKeys),
			Headers:     []string{},
			Disabled:    value.disabled,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func decodeJSONFile(path string, value any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, value); err != nil {
		return err
	}
	return nil
}

func commandFromSlice(values []string) (string, []string) {
	if len(values) == 0 {
		return "", []string{}
	}
	return sanitizeArg(values[0]), sanitizeArgs(values[1:])
}

func transport(configured, command, endpoint string) string {
	if configured != "" {
		return configured
	}
	if endpoint != "" {
		return "remote"
	}
	if command != "" {
		return "local"
	}
	return "unknown"
}

func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return sortedStringSlice(keys)
}

func sortedStringSlice(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return result
}

func sanitizeArgs(values []string) []string {
	result := make([]string, 0, len(values))
	redactNext := false
	for _, value := range values {
		if redactNext && !strings.HasPrefix(value, "-") {
			result = append(result, "[redacted]")
			redactNext = false
			continue
		}
		redactNext = strings.HasPrefix(value, "-") && sensitiveName(value)
		result = append(result, sanitizeArg(value))
	}
	return result
}

func sanitizeArg(value string) string {
	if strings.Contains(value, "://") {
		return safeURL(value)
	}
	name, _, found := strings.Cut(value, "=")
	if found && sensitiveName(name) {
		return name + "=[redacted]"
	}
	return value
}

func sensitiveName(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(value, "-", "_"))
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "api_key")
}

func safeURL(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "[redacted]"
	}
	parsed.User = nil
	parsed.Fragment = ""
	parsed.RawFragment = ""
	query := parsed.Query()
	for key := range query {
		query.Set(key, "[redacted]")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func unquoteTOMLString(value string) string {
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func unquoteSection(value string) string {
	return unquoteTOMLString(value)
}

func parseTOMLStringSlice(value string) []string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return []string{}
	}
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if inner == "" {
		return []string{}
	}
	parts := make([]string, 0)
	var current strings.Builder
	quoted := false
	escaped := false
	for _, char := range inner {
		switch {
		case escaped:
			current.WriteRune(char)
			escaped = false
		case char == '\\':
			current.WriteRune(char)
			escaped = true
		case char == '"':
			current.WriteRune(char)
			quoted = !quoted
		case char == ',' && !quoted:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}
	parts = append(parts, strings.TrimSpace(current.String()))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = unquoteTOMLString(strings.TrimSpace(part))
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
