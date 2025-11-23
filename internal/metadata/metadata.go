package metadata

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const metadataIndent = "  "

// BlockKind identifies the type of metadata block.
type BlockKind string

const (
	KindCollection BlockKind = "collection"
	KindGame       BlockKind = "game"
)

// Document represents a metadata.pegasus.txt file. The document keeps the
// original ordering of collection and game blocks so that it can be written
// back without losing information.
type Document struct {
	Blocks []*Block
}

// Block represents a collection or game block with its raw entries.
type Block struct {
	Kind    BlockKind
	Entries []*Entry
}

// Entry stores an individual name/value entry inside a block.
type Entry struct {
	Key    string
	Values []string
	Inline bool
}

var defaultFieldMapping = map[string]string{
	"assets.box_front": "assets.boxFront",
}

// Collection contains the parsed friendly view of a collection block.
type Collection struct {
	Name             string
	SortBy           string
	Extensions       []string
	IgnoreExtensions []string
	IgnoreFiles      []string
	Files            []string
	Launch           string
	WorkDir          string
	ShortName        string
	Summary          string
	Description      string
	Regex            []string
}

// Game contains a parsed friendly view of a game block.
type Game struct {
	Title       string
	SortBy      string
	Files       []string
	Developers  []string
	Publishers  []string
	Genres      []string
	Tags        []string
	Summary     string
	Description string
	Players     string
	Release     string
	Rating      string
	Launch      string
	WorkDir     string
	Assets      map[string]string
	Extra       map[string][]string
}

// ParseMetadataFile reads and parses a metadata.pegasus.txt file.
func ParseMetadataFile(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open metadata %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	doc := &Document{}
	var block *Block
	var lastEntry *Entry
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		raw = strings.TrimSuffix(raw, "\r")
		if lineNo == 1 {
			raw = strings.TrimPrefix(raw, "\ufeff")
		}

		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			lastEntry = nil
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			lastEntry = nil
			continue
		}

		firstRune := firstRune(raw)
		if firstRune != rune(0) && unicode.IsSpace(firstRune) {
			if lastEntry == nil {
				return nil, fmt.Errorf("metadata %s:%d: value without preceding key", path, lineNo)
			}
			value := strings.TrimSpace(raw)
			if value == "" {
				return nil, fmt.Errorf("metadata %s:%d: continuation value must not be empty", path, lineNo)
			}
			lastEntry.Values = append(lastEntry.Values, value)
			continue
		}

		colon := strings.IndexRune(raw, ':')
		if colon == -1 {
			return nil, fmt.Errorf("metadata %s:%d: expected key-value entry", path, lineNo)
		}

		key := strings.ToLower(strings.TrimSpace(raw[:colon]))
		if key == "" {
			return nil, fmt.Errorf("metadata %s:%d: invalid entry name", path, lineNo)
		}

		value := ""
		if colon+1 < len(raw) {
			value = strings.TrimSpace(raw[colon+1:])
		}

		key = normalizeKey(key)

		switch key {
		case string(KindCollection):
			block = &Block{Kind: KindCollection}
			doc.Blocks = append(doc.Blocks, block)
		case string(KindGame):
			block = &Block{Kind: KindGame}
			doc.Blocks = append(doc.Blocks, block)
		default:
			if block == nil {
				return nil, fmt.Errorf("metadata %s:%d: entry %s must belong to collection or game", path, lineNo, key)
			}
		}

		entry := &Entry{Key: key, Inline: value != ""}
		if value != "" {
			entry.Values = append(entry.Values, value)
		}
		block.Entries = append(block.Entries, entry)
		lastEntry = entry
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metadata %s: %w", path, err)
	}

	if len(doc.Blocks) == 0 {
		return nil, fmt.Errorf("metadata %s: no collection or game blocks found", path)
	}

	for idx, blk := range doc.Blocks {
		switch blk.Kind {
		case KindCollection:
			if entry := blk.Entry("collection"); entry == nil || len(entry.Values) == 0 {
				return nil, fmt.Errorf("metadata %s: collection block %d missing name", path, idx+1)
			}
		case KindGame:
			if entry := blk.Entry("game"); entry == nil || len(entry.Values) == 0 {
				return nil, fmt.Errorf("metadata %s: game block %d missing title", path, idx+1)
			}
		default:
			return nil, fmt.Errorf("metadata %s: unknown block type %q", path, blk.Kind)
		}
	}

	return doc, nil
}

// WriteMetadataFile serialises a Document back to disk following the Pegasus format.
func WriteMetadataFile(path string, doc *Document) error {
	if doc == nil {
		return fmt.Errorf("metadata document is nil")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("metadata output path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ensure metadata dir %s: %w", path, err)
	}

	var buf bytes.Buffer
	for i, blk := range doc.Blocks {
		if blk == nil {
			continue
		}
		if i > 0 {
			buf.WriteByte('\n')
		}
		for _, entry := range blk.Entries {
			if entry == nil {
				continue
			}
			buf.WriteString(entry.Key)
			buf.WriteString(":")
			start := 0
			if entry.Inline && len(entry.Values) > 0 {
				buf.WriteByte(' ')
				buf.WriteString(entry.Values[0])
				buf.WriteByte('\n')
				start = 1
			} else {
				buf.WriteByte('\n')
			}
			for idx := start; idx < len(entry.Values); idx++ {
				buf.WriteString(metadataIndent)
				buf.WriteString(entry.Values[idx])
				buf.WriteByte('\n')
			}
		}
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write metadata %s: %w", path, err)
	}
	return nil
}

// Entry returns the first entry for key.
func (b *Block) Entry(key string) *Entry {
	if b == nil {
		return nil
	}
	key = strings.ToLower(key)
	for _, entry := range b.Entries {
		if entry != nil && entry.Key == key {
			return entry
		}
	}
	return nil
}

// EntriesByKey returns all entries with the provided key.
func (b *Block) EntriesByKey(key string) []*Entry {
	if b == nil {
		return nil
	}
	key = strings.ToLower(key)
	var result []*Entry
	for _, entry := range b.Entries {
		if entry != nil && entry.Key == key {
			result = append(result, entry)
		}
	}
	return result
}

// Collections returns the typed view of all collection blocks.
func (d *Document) Collections() ([]Collection, error) {
	if d == nil {
		return nil, nil
	}
	var result []Collection
	for _, blk := range d.Blocks {
		if blk == nil || blk.Kind != KindCollection {
			continue
		}
		result = append(result, parseCollectionBlock(blk))
	}
	return result, nil
}

// Games returns the typed view of all game blocks.
func (d *Document) Games() ([]Game, error) {
	if d == nil {
		return nil, nil
	}
	var result []Game
	for _, blk := range d.Blocks {
		if blk == nil || blk.Kind != KindGame {
			continue
		}
		result = append(result, parseGameBlock(blk))
	}
	return result, nil
}

func parseCollectionBlock(blk *Block) Collection {
	var coll Collection
	for _, entry := range blk.Entries {
		if entry == nil {
			continue
		}
		switch entry.Key {
		case "collection":
			coll.Name = joinEntryValues(entry)
		case "sort-by":
			coll.SortBy = joinEntryValues(entry)
		case "shortname":
			coll.ShortName = joinEntryValues(entry)
		case "summary":
			coll.Summary = joinEntryValues(entry)
		case "description":
			coll.Description = joinEntryValues(entry)
		case "extensions", "extension":
			coll.Extensions = append(coll.Extensions, parseCSV(entry.Values)...)
		case "ignore-extension", "ignore-extensions":
			coll.IgnoreExtensions = append(coll.IgnoreExtensions, parseCSV(entry.Values)...)
		case "ignore-file", "ignore-files":
			coll.IgnoreFiles = append(coll.IgnoreFiles, cloneValues(entry.Values)...)
		case "files", "file":
			coll.Files = append(coll.Files, cloneValues(entry.Values)...)
		case "launch", "command":
			coll.Launch = joinEntryValues(entry)
		case "workdir", "cwd":
			coll.WorkDir = joinEntryValues(entry)
		case "regex":
			coll.Regex = append(coll.Regex, cloneValues(entry.Values)...)
		}
	}
	return coll
}

func parseGameBlock(blk *Block) Game {
	game := Game{}
	for _, entry := range blk.Entries {
		if entry == nil {
			continue
		}
		switch entry.Key {
		case "game":
			game.Title = joinEntryValues(entry)
		case "sort-by", "sort_name", "sort_title":
			game.SortBy = joinEntryValues(entry)
		case "file", "files":
			game.Files = append(game.Files, cloneValues(entry.Values)...)
		case "developer", "developers":
			game.Developers = append(game.Developers, parseCSV(entry.Values)...)
		case "publisher", "publishers":
			game.Publishers = append(game.Publishers, parseCSV(entry.Values)...)
		case "genre", "genres":
			game.Genres = append(game.Genres, parseCSV(entry.Values)...)
		case "tag", "tags":
			game.Tags = append(game.Tags, parseCSV(entry.Values)...)
		case "summary":
			game.Summary = joinEntryValues(entry)
		case "description":
			game.Description = joinEntryValues(entry)
		case "players":
			game.Players = joinEntryValues(entry)
		case "release":
			game.Release = joinEntryValues(entry)
		case "rating":
			game.Rating = joinEntryValues(entry)
		case "launch", "command":
			game.Launch = joinEntryValues(entry)
		case "workdir", "cwd":
			game.WorkDir = joinEntryValues(entry)
		default:
			if strings.HasPrefix(entry.Key, "assets.") {
				assetName := strings.TrimPrefix(entry.Key, "assets.")
				if assetName != "" {
					if game.Assets == nil {
						game.Assets = make(map[string]string)
					}
					game.Assets[assetName] = joinEntryValues(entry)
				}
				continue
			}
			if game.Extra == nil {
				game.Extra = make(map[string][]string)
			}
			game.Extra[entry.Key] = append(game.Extra[entry.Key], cloneValues(entry.Values)...)
		}
	}
	return game
}

func joinEntryValues(entry *Entry) string {
	if entry == nil {
		return ""
	}
	return strings.Join(entry.Values, "\n")
}

func parseCSV(values []string) []string {
	var out []string
	for _, value := range values {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func cloneValues(values []string) []string {
	var out []string
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func normalizeKey(in string) string {
	if v, ok := defaultFieldMapping[in]; ok {
		return v
	}
	return in
}
