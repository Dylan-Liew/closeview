package library

import (
	"os"
	"path/filepath"
)

type Library struct {
	skillRoots []skillRoot
	mcpConfigs []mcpConfig
}

type mcpConfig struct {
	source string
	path   string
	label  string
}

type SourceStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Count     int    `json:"count"`
	Error     string `json:"error,omitempty"`
}

func NewDefault() (*Library, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	openCodeConfig := os.Getenv("CLOSEVIEW_OPENCODE_CONFIG_HOME")
	if openCodeConfig == "" {
		openCodeConfig = filepath.Join(configHome, "opencode")
	}
	codexHome := firstEnvironmentValue("CLOSEVIEW_CODEX_HOME", "CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	claudeHome := os.Getenv("CLOSEVIEW_CLAUDE_HOME")
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	claudeConfig := os.Getenv("CLOSEVIEW_CLAUDE_CONFIG_FILE")
	if claudeConfig == "" {
		claudeConfig = filepath.Join(home, ".claude.json")
	}

	return &Library{
		skillRoots: []skillRoot{
			{source: "opencode", path: filepath.Join(openCodeConfig, "skills")},
			{source: "codex", path: filepath.Join(codexHome, "skills")},
			{source: "claude", path: filepath.Join(claudeHome, "skills")},
		},
		mcpConfigs: []mcpConfig{
			{source: "opencode", path: filepath.Join(openCodeConfig, "opencode.json"), label: "opencode.json"},
			{source: "codex", path: filepath.Join(codexHome, "config.toml"), label: "config.toml"},
			{source: "claude", path: claudeConfig, label: ".claude.json"},
		},
	}, nil
}

func firstEnvironmentValue(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func sourceIndex(source string) int {
	switch source {
	case "opencode":
		return 0
	case "codex":
		return 1
	case "claude":
		return 2
	default:
		return 3
	}
}
