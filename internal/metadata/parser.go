package metadata

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Game describes the subset of Pegasus metadata we care about.
type Game struct {
	Name        string
	Files       []string
	Description string
}

// Document represents a metadata file and its games.
type Document struct {
	Collection string
	Games      []Game
}

// Parse reads a Pegasus metadata file from disk.
func Parse(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open metadata %s: %w", path, err)
	}
	defer f.Close()

	doc := &Document{}
	var current *Game

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		colon := strings.Index(line, ":")
		if colon == -1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])

		switch key {
		case "collection":
			if doc.Collection == "" {
				doc.Collection = value
			}
		case "game":
			if current != nil {
				doc.Games = append(doc.Games, *current)
			}
			current = &Game{Name: value}
		case "file":
			if current == nil {
				continue
			}
			current.Files = append(current.Files, value)
		case "description":
			if current == nil {
				continue
			}
			current.Description = value
		default:
			// Ignore other keys for now.
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metadata %s: %w", path, err)
	}

	if current != nil {
		doc.Games = append(doc.Games, *current)
	}

	return doc, nil
}
