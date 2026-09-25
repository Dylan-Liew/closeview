package library

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var ErrNotFound = errors.New("library item not found")

type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Scope       string `json:"scope"`
	Path        string `json:"path"`
}

type SkillDetail struct {
	Skill   Skill  `json:"skill"`
	Content string `json:"content"`
}

type SkillCatalog struct {
	Skills  []Skill        `json:"skills"`
	Sources []SourceStatus `json:"sources"`
}

type skillRoot struct {
	source string
	path   string
}

func (l *Library) ListSkills(context.Context) SkillCatalog {
	catalog := SkillCatalog{Skills: []Skill{}, Sources: []SourceStatus{}}
	for _, root := range l.skillRoots {
		skills, err := skillsUnder(root)
		status := SourceStatus{Name: root.source, Available: err == nil, Count: len(skills)}
		if err != nil {
			status.Error = err.Error()
		}
		catalog.Sources = append(catalog.Sources, status)
		catalog.Skills = append(catalog.Skills, skills...)
	}
	sort.SliceStable(catalog.Skills, func(i, j int) bool {
		if catalog.Skills[i].Source != catalog.Skills[j].Source {
			return sourceIndex(catalog.Skills[i].Source) < sourceIndex(catalog.Skills[j].Source)
		}
		if catalog.Skills[i].Name != catalog.Skills[j].Name {
			return catalog.Skills[i].Name < catalog.Skills[j].Name
		}
		return catalog.Skills[i].Path < catalog.Skills[j].Path
	})
	return catalog
}

func (l *Library) GetSkill(_ context.Context, id string) (SkillDetail, error) {
	catalog := l.ListSkills(context.Background())
	for _, skill := range catalog.Skills {
		if skill.ID != id {
			continue
		}
		root := l.skillRootFor(skill.Source)
		if root == nil {
			return SkillDetail{}, ErrNotFound
		}
		path := filepath.Join(root.path, filepath.FromSlash(skill.Path), "SKILL.md")
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return SkillDetail{}, ErrNotFound
			}
			return SkillDetail{}, fmt.Errorf("read %s skill: %w", skill.Source, err)
		}
		_, body := parseSkillFrontmatter(string(content))
		return SkillDetail{Skill: skill, Content: strings.TrimSpace(body)}, nil
	}
	return SkillDetail{}, ErrNotFound
}

func (l *Library) skillRootFor(source string) *skillRoot {
	for index := range l.skillRoots {
		if l.skillRoots[index].source == source {
			return &l.skillRoots[index]
		}
	}
	return nil
}

func skillsUnder(root skillRoot) ([]Skill, error) {
	if _, err := os.Stat(root.path); err != nil {
		if os.IsNotExist(err) {
			return []Skill{}, nil
		}
		return nil, err
	}

	skills := make([]Skill, 0)
	var walkError error
	err := filepath.WalkDir(root.path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			walkError = err
			return nil
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		relative, err := filepath.Rel(root.path, filepath.Dir(path))
		if err != nil {
			walkError = err
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			walkError = err
			return nil
		}
		frontmatter, _ := parseSkillFrontmatter(string(content))
		relative = filepath.ToSlash(relative)
		name := strings.TrimSpace(frontmatter["name"])
		if name == "" {
			name = filepath.Base(relative)
		}
		skills = append(skills, Skill{
			ID:          encodeLibraryID(root.source, relative),
			Name:        name,
			Description: strings.TrimSpace(frontmatter["description"]),
			Source:      root.source,
			Scope:       skillScope(root.source, relative),
			Path:        relative,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if walkError != nil {
		return nil, walkError
	}
	return skills, nil
}

func parseSkillFrontmatter(content string) (map[string]string, string) {
	metadata := make(map[string]string)
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return metadata, content
	}

	var blockKey string
	var blockStyle string
	var blockLines []string
	flushBlock := func() {
		if blockKey == "" {
			return
		}
		separator := "\n"
		if strings.HasPrefix(blockStyle, ">") {
			separator = " "
		}
		metadata[blockKey] = strings.TrimSpace(strings.Join(blockLines, separator))
		blockKey = ""
		blockStyle = ""
		blockLines = nil
	}

	for index := 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if blockKey != "" && (line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			blockLines = append(blockLines, trimmed)
			continue
		}
		flushBlock()
		if trimmed == "---" {
			return metadata, strings.Join(lines[index+1:], "\n")
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == ">" || value == ">>" || value == "|" || value == ">-" || value == "|-" {
			blockKey = key
			blockStyle = value
			continue
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		metadata[key] = value
	}
	flushBlock()
	return metadata, ""
}

func skillScope(source, relative string) string {
	if source == "codex" && (relative == ".system" || strings.HasPrefix(relative, ".system/")) {
		return "system"
	}
	return "user"
}

func encodeLibraryID(source, relative string) string {
	return source + "." + base64.RawURLEncoding.EncodeToString([]byte(relative))
}
