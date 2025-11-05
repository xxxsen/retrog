package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMetadataMultiFile(t *testing.T) {
	content := `# Sample metadata
collection: Test Collection

game: First Game
file: 
  rom1.zip
  rom1b.zip
genre: Action, Adventure
description:
  Line one
  Line two
`
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "metadata.pegasus.txt")
	if err := os.WriteFile(metaPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	doc, err := Parse(metaPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	assert.Equal(t, 1, len(doc.Games))
	g1 := doc.Games[0]
	assert.Equal(t, g1.Files, []string{"rom1.zip", "rom1b.zip"})
	assert.Equal(t, g1.Genres, []string{"Action", "Adventure"})
	assert.Equal(t, g1.Description, "Line one\nLine two")
}

func TestParseMetadataBasic(t *testing.T) {
	t.Parallel()

	content := `# Sample metadata
collection: Test Collection

game: First Game
file: rom1.zip
file:
  rom1b.zip
description:
  Line one
  Line two
developer: Dev Studio
publisher: Pub House
genre: Action, Adventure
release: 2001-01-01
assets.boxart: cover.png

game:
  Second Game
file: second.rom
genre:
  RPG
# trailing comment
`

	dir := t.TempDir()
	metaPath := filepath.Join(dir, "metadata.pegasus.txt")
	if err := os.WriteFile(metaPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	doc, err := Parse(metaPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := doc.Collection; got != "Test Collection" {
		t.Fatalf("Collection = %q, want %q", got, "Test Collection")
	}

	if len(doc.Games) != 2 {
		t.Fatalf("expected 2 games, got %d", len(doc.Games))
	}

	first := doc.Games[0]
	if first.Name != "First Game" {
		t.Fatalf("first game name = %q, want %q", first.Name, "First Game")
	}
	if desc := first.Description; desc != "Line one\nLine two" {
		t.Fatalf("first game description = %q", desc)
	}
	wantFiles := []string{"rom1.zip", "rom1b.zip"}
	if len(first.Files) != len(wantFiles) {
		t.Fatalf("first game files length = %d, want %d", len(first.Files), len(wantFiles))
	}
	for i, want := range wantFiles {
		if first.Files[i] != want {
			t.Fatalf("first game file %d = %q, want %q", i, first.Files[i], want)
		}
	}
	if len(first.Genres) != 2 || first.Genres[0] != "Action" || first.Genres[1] != "Adventure" {
		t.Fatalf("first game genres = %#v", first.Genres)
	}
	if first.Developer != "Dev Studio" || first.Publisher != "Pub House" {
		t.Fatalf("developer/publisher mismatch: %q/%q", first.Developer, first.Publisher)
	}
	if first.Release != "2001-01-01" {
		t.Fatalf("first game release = %q", first.Release)
	}
	if first.Assets == nil || first.Assets["boxart"] != "cover.png" {
		t.Fatalf("first game assets = %#v", first.Assets)
	}

	second := doc.Games[1]
	if second.Name != "Second Game" {
		t.Fatalf("second game name = %q, want %q", second.Name, "Second Game")
	}
	if len(second.Files) != 1 || second.Files[0] != "second.rom" {
		t.Fatalf("second game files = %#v", second.Files)
	}
	if len(second.Genres) != 1 || second.Genres[0] != "RPG" {
		t.Fatalf("second game genres = %#v", second.Genres)
	}
}

func TestParseMetadataErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"value without entry": "  orphan value",
		"game without name":   "game:",
		"missing colon":       "collection Test",
	}

	for name, content := range tests {
		content := content
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			metaPath := filepath.Join(dir, "metadata.pegasus.txt")
			if err := os.WriteFile(metaPath, []byte(content), 0o644); err != nil {
				t.Fatalf("write metadata: %v", err)
			}

			if _, err := Parse(metaPath); err == nil {
				t.Fatalf("expected error but got nil")
			}
		})
	}
}
