package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMetadataDocument(t *testing.T) {
	t.Parallel()

	content := `# Sample metadata
collection: Super Nintendo Entertainment System
sort-by: SNES
extensions: 7z, bin, smc
ignore-extensions:
  bak, tmp
ignore-files:
  buggygame.bin
files: titles.db
launch: run "{file.path}"
workdir: /roms
shortname: snes
summary:
  Summary line one
  Summary line two
description:
  Description line
  two
regex:
  ^kof

# Game entries

game: First Game
file: rom1.zip
files:
  rom1b.zip
developer: Dev Studio, Dev Studio 2
publisher:
  Pub House
genre: Action, Adventure
tag: Local
summary: Short summary
description:
  Line one
  Line two
players: 1-2
release: 1990-01-01
rating: 80%
command: launch "{file.path}"
cwd: /roms
assets.boxFront: cover.png
assets.box_front: alt_cover.png
assets.logo:
  logo.png
x-id: custom
`

	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.pegasus.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	doc, err := ParseMetadataFile(path)
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}

	if got := len(doc.Blocks); got != 2 {
		t.Fatalf("expected 2 blocks, got %d", got)
	}

	collections, err := doc.Collections()
	if err != nil {
		t.Fatalf("collections returned error: %v", err)
	}
	if assert.Len(t, collections, 1) {
		coll := collections[0]
		assert.Equal(t, "Super Nintendo Entertainment System", coll.Name)
		assert.Equal(t, "SNES", coll.SortBy)
		assert.Equal(t, []string{"7z", "bin", "smc"}, coll.Extensions)
		assert.Equal(t, []string{"bak", "tmp"}, coll.IgnoreExtensions)
		assert.Equal(t, []string{"buggygame.bin"}, coll.IgnoreFiles)
		assert.Equal(t, []string{"titles.db"}, coll.Files)
		assert.Equal(t, "run \"{file.path}\"", coll.Launch)
		assert.Equal(t, "/roms", coll.WorkDir)
		assert.Equal(t, "snes", coll.ShortName)
		assert.Equal(t, "Summary line one\nSummary line two", coll.Summary)
		assert.Equal(t, "Description line\ntwo", coll.Description)
		assert.Equal(t, []string{"^kof"}, coll.Regex)
	}

	games, err := doc.Games()
	if err != nil {
		t.Fatalf("games returned error: %v", err)
	}
	if assert.Len(t, games, 1) {
		game := games[0]
		assert.Equal(t, "First Game", game.Title)
		assert.Equal(t, []string{"rom1.zip", "rom1b.zip"}, game.Files)
		assert.Equal(t, []string{"Dev Studio", "Dev Studio 2"}, game.Developers)
		assert.Equal(t, []string{"Pub House"}, game.Publishers)
		assert.Equal(t, []string{"Action", "Adventure"}, game.Genres)
		assert.Equal(t, []string{"Local"}, game.Tags)
		assert.Equal(t, "Short summary", game.Summary)
		assert.Equal(t, "Line one\nLine two", game.Description)
		assert.Equal(t, "1-2", game.Players)
		assert.Equal(t, "1990-01-01", game.Release)
		assert.Equal(t, "80%", game.Rating)
		assert.Equal(t, "launch \"{file.path}\"", game.Launch)
		assert.Equal(t, "/roms", game.WorkDir)
		assert.Equal(t, map[string]string{"boxfront": "alt_cover.png", "logo": "logo.png"}, game.Assets)
		assert.Equal(t, []string{"custom"}, game.Extra["x-id"])
	}
}

func TestWriteMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	content := `collection: Sample
shortname: sample

# comment

game: Example
file: example.zip
`

	dir := t.TempDir()
	src := filepath.Join(dir, "metadata.pegasus.txt")
	dst := filepath.Join(dir, "metadata.pegasus.txt.fix")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	doc, err := ParseMetadataFile(src)
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	if err := WriteMetadataFile(dst, doc); err != nil {
		t.Fatalf("write metadata returned error: %v", err)
	}

	roundTrip, err := ParseMetadataFile(dst)
	if err != nil {
		t.Fatalf("parse round trip error: %v", err)
	}

	collections, _ := roundTrip.Collections()
	if assert.Len(t, collections, 1) {
		assert.Equal(t, "Sample", collections[0].Name)
		assert.Equal(t, "sample", collections[0].ShortName)
	}
	games, _ := roundTrip.Games()
	if assert.Len(t, games, 1) {
		assert.Equal(t, []string{"example.zip"}, games[0].Files)
	}
}

func TestMetadataParseErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"value without entry": "  orphan value",
		"entry before block":  "sort-by: First",
		"missing colon":       "collection Test",
		"game without title":  "game:",
	}

	for name, content := range tests {
		content := content
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "metadata.pegasus.txt")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write metadata: %v", err)
			}

			if _, err := ParseMetadataFile(path); err == nil {
				t.Fatalf("expected error but got nil")
			}
		})
	}
}
