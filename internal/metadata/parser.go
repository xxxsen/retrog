package metadata

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Game describes the subset of Pegasus metadata we care about.
type Game struct {
	Name        string
	Files       []string
	Description string
	Assets      map[string]string
	Developer   string
	Publisher   string
	Genres      []string
	Release     string
}

// Document represents a metadata file and its games.
type Document struct {
	Cat        string
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
	var lastKey string
	scanner := bufio.NewScanner(f)
	lineNo := 0

	commitGame := func() error {
		if current != nil {
			if strings.TrimSpace(current.Name) == "" {
				return fmt.Errorf("metadata %s:%d: game entry missing name", path, lineNo)
			}
			doc.Games = append(doc.Games, *current)
			current = nil
		}
		return nil
	}

	ensureGame := func() error {
		if current == nil {
			return fmt.Errorf("metadata %s:%d: entry requires active game", path, lineNo)
		}
		return nil
	}

	appendValue := func(key, value string) error {
		switch {
		case key == "collection":
			if value != "" && doc.Collection == "" {
				doc.Collection = value
			}
		case key == "game":
			if err := ensureGame(); err != nil {
				return err
			}
			if current.Name == "" {
				current.Name = value
			} else {
				current.Name += "\n" + value
			}
		case key == "file":
			if err := ensureGame(); err != nil {
				return err
			}
			current.Files = append(current.Files, value)
		case key == "description":
			if err := ensureGame(); err != nil {
				return err
			}
			if current.Description == "" {
				current.Description = value
			} else {
				current.Description += "\n" + value
			}
		case key == "developer":
			if err := ensureGame(); err != nil {
				return err
			}
			current.Developer = value
		case key == "publisher":
			if err := ensureGame(); err != nil {
				return err
			}
			current.Publisher = value
		case key == "release":
			if err := ensureGame(); err != nil {
				return err
			}
			current.Release = strings.TrimSpace(value)
		case key == "genre":
			if err := ensureGame(); err != nil {
				return err
			}
			for _, part := range strings.Split(value, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					current.Genres = append(current.Genres, trimmed)
				}
			}
		case strings.HasPrefix(key, "assets."):
			if err := ensureGame(); err != nil {
				return err
			}
			if current.Assets == nil {
				current.Assets = make(map[string]string)
			}
			assetKey := strings.TrimPrefix(key, "assets.")
			if prev, ok := current.Assets[assetKey]; ok && prev != "" {
				current.Assets[assetKey] = prev + "\n" + value
			} else {
				current.Assets[assetKey] = value
			}
		default:
			// Ignore unrecognised entries.
		}
		return nil
	}

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if len(raw) == 0 {
			continue
		}

		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(raw, "#") {
			continue
		}

		firstRune, _ := utf8.DecodeRuneInString(raw)
		if unicode.IsSpace(firstRune) {
			if lastKey == "" {
				return nil, fmt.Errorf("metadata %s:%d: value without preceding entry", path, lineNo)
			}
			val := strings.TrimSpace(raw)
			if val == "" {
				return nil, fmt.Errorf("metadata %s:%d: continuation value must not be empty", path, lineNo)
			}
			if err := appendValue(lastKey, val); err != nil {
				return nil, err
			}
			continue
		}

		colon := strings.IndexRune(raw, ':')
		if colon == -1 {
			return nil, fmt.Errorf("metadata %s:%d: expected key-value entry", path, lineNo)
		}

		key := strings.ToLower(strings.TrimSpace(raw[:colon]))
		if key == "" || strings.ContainsRune(key, ':') {
			return nil, fmt.Errorf("metadata %s:%d: invalid entry name", path, lineNo)
		}

		value := ""
		if colon+1 < len(raw) {
			value = strings.TrimSpace(raw[colon+1:])
		}

		if key == "game" {
			if err := commitGame(); err != nil {
				return nil, err
			}
			current = &Game{}
		}

		lastKey = key

		if value == "" {
			continue
		}

		if err := appendValue(key, value); err != nil {
			return nil, err
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metadata %s: %w", path, err)
	}

	if err := commitGame(); err != nil {
		return nil, err
	}
	return doc, nil
}
