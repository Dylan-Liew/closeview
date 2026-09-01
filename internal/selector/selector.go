package selector

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Dylan-Liew/closeview/internal/store"
)

func SelectPath(prompt string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		entries, err := pathCandidates(".")
		if err != nil {
			return "", err
		}

		fmt.Println(prompt)
		fmt.Println("Type a path, or pick a nearby file/directory:")
		for i, entry := range entries {
			fmt.Printf("  %2d. %s\n", i+1, entry)
		}
		fmt.Print("> ")

		input, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if number, err := strconv.Atoi(input); err == nil {
			if number < 1 || number > len(entries) {
				fmt.Println("selection out of range")
				continue
			}
			return entries[number-1], nil
		}
		return input, nil
	}
}

func SelectSession(prompt string, sessions []store.Session) (store.Session, error) {
	reader := bufio.NewReader(os.Stdin)
	filtered := sessions

	for {
		fmt.Println(prompt)
		printSessionOptions(filtered)
		fmt.Print("Select number, type search text, or q to quit: ")

		input, err := reader.ReadString('\n')
		if err != nil {
			return store.Session{}, err
		}
		input = strings.TrimSpace(input)
		if input == "q" || input == "quit" {
			return store.Session{}, fmt.Errorf("selection cancelled")
		}
		if input == "" {
			continue
		}
		if number, err := strconv.Atoi(input); err == nil {
			if number < 1 || number > len(filtered) {
				fmt.Println("selection out of range")
				continue
			}
			return filtered[number-1], nil
		}

		filtered = filterSessions(sessions, input)
		if len(filtered) == 0 {
			fmt.Println("no matches; showing all sessions")
			filtered = sessions
		}
	}
}

func SelectSessions(prompt string, sessions []store.Session) ([]store.Session, error) {
	reader := bufio.NewReader(os.Stdin)
	filtered := sessions
	for {
		fmt.Println(prompt)
		printSessionOptions(filtered)
		fmt.Print("Select numbers separated by commas, 'all', search text, or q to quit: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		input = strings.TrimSpace(input)
		if input == "q" || input == "quit" {
			return nil, fmt.Errorf("selection cancelled")
		}
		if input == "" {
			continue
		}
		if input == "all" {
			return filtered, nil
		}
		if strings.Contains(input, ",") || isNumber(input) {
			selected, ok := pickSessionNumbers(filtered, input)
			if ok {
				return selected, nil
			}
			fmt.Println("selection out of range")
			continue
		}
		filtered = filterSessions(sessions, input)
		if len(filtered) == 0 {
			fmt.Println("no matches; showing all sessions")
			filtered = sessions
		}
	}
}

func pickSessionNumbers(sessions []store.Session, input string) ([]store.Session, bool) {
	parts := strings.Split(input, ",")
	selected := make([]store.Session, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || number < 1 || number > len(sessions) {
			return nil, false
		}
		if seen[number] {
			continue
		}
		seen[number] = true
		selected = append(selected, sessions[number-1])
	}
	return selected, len(selected) > 0
}

func isNumber(input string) bool {
	_, err := strconv.Atoi(input)
	return err == nil
}

func pathCandidates(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".git") {
			continue
		}
		path := filepath.Join(root, name)
		if entry.IsDir() || supportedExt(path) {
			candidates = append(candidates, path)
		}
	}
	sort.Strings(candidates)
	if len(candidates) > 30 {
		candidates = candidates[:30]
	}
	return candidates, nil
}

func supportedExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".json", ".jsonl", ".ndjson", ".txt", ".log":
		return true
	default:
		return false
	}
}

func printSessionOptions(sessions []store.Session) {
	for i, session := range sessions {
		id := session.ID
		if len(id) > 10 {
			id = id[:10]
		}
		fmt.Printf("  %2d. %-10s %-12s %4d  %s\n", i+1, id, session.Source, session.MessageCount, session.Title)
	}
}

func filterSessions(sessions []store.Session, query string) []store.Session {
	query = strings.ToLower(query)
	var filtered []store.Session
	for _, session := range sessions {
		text := strings.ToLower(session.ID + " " + session.Source + " " + session.Title + " " + session.ProjectPath)
		if strings.Contains(text, query) {
			filtered = append(filtered, session)
		}
	}
	return filtered
}
