package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxxsen/retrog/internal/dat"
)

func makeZip(t *testing.T, files map[string][]byte) []*zip.File {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip reader: %v", err)
	}
	// Make a copy of []*zip.File so caller can mutate it without affecting reader.
	filesCopy := make([]*zip.File, len(reader.File))
	copy(filesCopy, reader.File)
	return filesCopy
}

func TestValidateRomArchivePass(t *testing.T) {
	dataA := []byte("abc")
	dataB := []byte("12345")

	game := &dat.Game{
		Roms: []dat.Rom{
			{Name: "a.bin", Size: int64(len(dataA)), CRC: crcHex(dataA)},
			{Name: "path/b.bin", Size: int64(len(dataB)), CRC: crcHex(dataB)},
		},
	}

	files := makeZip(t, map[string][]byte{
		"a.bin":                dataA,
		filepath.Join("path", "b.bin"): dataB,
	})

	issues := validateRomArchive(game, files)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestValidateRomArchiveDetectsProblems(t *testing.T) {
	game := &dat.Game{
		Roms: []dat.Rom{
			{Name: "missing.rom", Size: 2, CRC: "d955be2d"},
			{Name: "badsize.rom", Size: 4, CRC: "b2a5c80f"},
			{Name: "badcrc.rom", Size: 3, CRC: "ffffffff"},
		},
	}

	files := makeZip(t, map[string][]byte{
		"badsize.rom": []byte("toolong"),
		"badcrc.rom":  []byte("abc"),
	})

	issues := validateRomArchive(game, files)
	if len(issues) != 4 {
		t.Fatalf("expected 4 issues, got %v", issues)
	}
}

func TestValidateRomArchiveChoosesMatchingCandidate(t *testing.T) {
	right := []byte("abc")
	wrong := []byte("abcd")

	game := &dat.Game{
		Roms: []dat.Rom{
			{Name: "dup.bin", Size: int64(len(right)), CRC: crcHex(right)},
		},
	}

	files := makeZip(t, map[string][]byte{
		filepath.Join("a", "dup.bin"): wrong,
		filepath.Join("b", "dup.bin"): right,
	})

	issues := validateRomArchive(game, files)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestValidateRomArchiveNoMatchingCandidate(t *testing.T) {
	game := &dat.Game{
		Roms: []dat.Rom{
			{Name: "dup.bin", Size: 3, CRC: "00000000"},
		},
	}

	files := makeZip(t, map[string][]byte{
		filepath.Join("a", "dup.bin"): []byte("abc"),
		filepath.Join("b", "dup.bin"): []byte("abcd"),
	})

	issues := validateRomArchive(game, files)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %v", issues)
	}
}

func crcHex(data []byte) string {
	sum := crc32.ChecksumIEEE(data)
	return strings.ToLower(fmt.Sprintf("%08x", sum))
}
