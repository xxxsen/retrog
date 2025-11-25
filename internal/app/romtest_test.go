package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"os"
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
		"a.bin":                        dataA,
		filepath.Join("path", "b.bin"): dataB,
	})

	issues, skipped, mismatch := validateRomArchive(game, files)
	if len(issues) != 0 || skipped || mismatch {
		t.Fatalf("expected no issues, got %v skipped %v mismatch %v", issues, skipped, mismatch)
	}
}

func TestValidateRomArchiveDetectsProblems(t *testing.T) {
	game := &dat.Game{
		Roms: []dat.Rom{
			{Name: "missing.rom", Size: 600, CRC: "d955be2d"},
			{Name: "badsize.rom", Size: 600, CRC: "b2a5c80f"},
			{Name: "badcrc.rom", Size: 600, CRC: "ffffffff"},
		},
	}

	files := makeZip(t, map[string][]byte{
		"badsize.rom": []byte("toolong"),
		"badcrc.rom":  []byte("abc"),
	})

	issues, skipped, mismatch := validateRomArchive(game, files)
	if len(issues) != 5 || skipped || mismatch {
		t.Fatalf("expected 5 issues, got %v skipped %v mismatch %v", issues, skipped, mismatch)
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

	issues, skipped, mismatch := validateRomArchive(game, files)
	if len(issues) != 0 || skipped || mismatch {
		t.Fatalf("expected no issues, got %v skipped %v mismatch %v", issues, skipped, mismatch)
	}
}

func TestValidateRomArchiveNoMatchingCandidate(t *testing.T) {
	game := &dat.Game{
		Roms: []dat.Rom{
			{Name: "dup.bin", Size: 600, CRC: "00000000"},
		},
	}

	files := makeZip(t, map[string][]byte{
		filepath.Join("a", "dup.bin"): []byte("abc"),
		filepath.Join("b", "dup.bin"): []byte("abcd"),
	})

	issues, skipped, mismatch := validateRomArchive(game, files)
	if len(issues) != 1 || skipped || mismatch {
		t.Fatalf("expected 1 issue, got %v skipped %v mismatch %v", issues, skipped, mismatch)
	}
}

func crcHex(data []byte) string {
	sum := crc32.ChecksumIEEE(data)
	return strings.ToLower(fmt.Sprintf("%08x", sum))
}

func TestLoadRomDefinitionsFbneo(t *testing.T) {
	dir := t.TempDir()
	datPath := filepath.Join(dir, "fbneo.dat")
	content := `<?xml version="1.0"?>
<datafile>
	<header>
		<name>fbneo</name>
	</header>
	<game name="game1">
		<description>Game 1</description>
		<rom name="a.bin" size="3" crc="616263"/>
	</game>
</datafile>`
	if err := os.WriteFile(datPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write dat: %v", err)
	}
	cmd := &RomTestCommand{kind: "fbneo", datPath: datPath}
	defs, err := cmd.loadRomDefinitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	def, ok := defs["game1"]
	if !ok {
		t.Fatalf("missing game1 definition")
	}
	if len(def.Roms) != 1 || def.Roms[0].Name != "a.bin" {
		t.Fatalf("unexpected roms: %+v", def.Roms)
	}
}

func TestLoadRomDefinitionsMame(t *testing.T) {
	dir := t.TempDir()
	datPath := filepath.Join(dir, "mame.dat")
	content := `<?xml version="1.0"?>
<!DOCTYPE datafile PUBLIC "-//Logiqx//DTD ROM Management Datafile//EN" "http://www.logiqx.com/Dats/datafile.dtd">
<datafile>
	<header><name>MAME</name></header>
	<machine name="mamegame">
		<description>Game</description>
		<rom name="a.bin" size="1" crc="00"/>
	</machine>
</datafile>`
	if err := os.WriteFile(datPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write dat: %v", err)
	}
	cmd := &RomTestCommand{kind: "mame", datPath: datPath}
	defs, err := cmd.loadRomDefinitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	if _, ok := defs["mamegame"]; !ok {
		t.Fatalf("missing mamegame definition")
	}
}

func TestCollectTargetsFiltersByExt(t *testing.T) {
	dir := t.TempDir()
	allowed := filepath.Join(dir, "game.zip")
	skipped := filepath.Join(dir, "game.txt")
	if err := os.WriteFile(allowed, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write allowed: %v", err)
	}
	if err := os.WriteFile(skipped, []byte("skip"), 0o644); err != nil {
		t.Fatalf("write skipped: %v", err)
	}
	cmd := &RomTestCommand{dirPath: dir, exts: "zip"}
	allowedSet, _ := normalizeExts(cmd.exts)
	targets, err := cmd.collectTargets(allowedSet)
	if err != nil {
		t.Fatalf("collect targets: %v", err)
	}
	if len(targets) != 1 || targets[0] != allowed {
		t.Fatalf("unexpected targets: %v", targets)
	}
}

func TestCollectBiosPaths(t *testing.T) {
	dir := t.TempDir()
	bios := filepath.Join(dir, "neogeo.zip")
	if err := os.WriteFile(bios, []byte("bios"), 0o644); err != nil {
		t.Fatalf("write bios: %v", err)
	}
	cmd := &RomTestCommand{biosDir: dir, exts: "zip"}
	allowedSet, _ := normalizeExts(cmd.exts)
	paths, err := cmd.collectBiosPaths(allowedSet)
	if err != nil {
		t.Fatalf("collect bios paths: %v", err)
	}
	if got, ok := paths["neogeo"]; !ok || got != bios {
		t.Fatalf("unexpected bios map: %v", paths)
	}
}

func TestValidateFile(t *testing.T) {
	dir := t.TempDir()
	data := []byte("abc")
	crc := crcHex(data)
	defs := map[string]romDefinition{
		"good": {Name: "good", Roms: []dat.Rom{{Name: "a.bin", Size: int64(len(data)), CRC: crc}}},
		"bad":  {Name: "bad", Roms: []dat.Rom{{Name: "missing.bin", Size: 600}}},
	}

	goodZip := filepath.Join(dir, "good.zip")
	if err := createZipFile(goodZip, map[string][]byte{"a.bin": data}); err != nil {
		t.Fatalf("create good zip: %v", err)
	}
	badZip := filepath.Join(dir, "bad.zip")
	if err := createZipFile(badZip, map[string][]byte{"other.bin": data}); err != nil {
		t.Fatalf("create bad zip: %v", err)
	}

	cmd := &RomTestCommand{}
	nameToPath := map[string]string{
		"good": "good.zip",
		"bad":  "bad.zip",
	}

	if issues, parent, skipped, mismatch, parentMissing := cmd.validateFile(defs, nameToPath, goodZip); len(issues) != 0 || parent != "" || skipped || mismatch || parentMissing {
		t.Fatalf("expected no issues, got %v (parent %s, skipped %v, mismatch %v, parentMissing %v)", issues, parent, skipped, mismatch, parentMissing)
	}
	nameToPath["bad"] = badZip
	if issues, parent, skipped, mismatch, parentMissing := cmd.validateFile(defs, nameToPath, badZip); len(issues) == 0 || parent != "" || skipped || mismatch || parentMissing {
		t.Fatalf("expected issues for bad zip")
	}
}

func TestValidateFileParentMissing(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("a"), 600)
	crc := crcHex(data)
	defs := map[string]romDefinition{
		"child":  {Name: "child", Parent: "parent", Roms: []dat.Rom{{Name: "a.bin", Size: int64(len(data)), CRC: crc}}},
		"parent": {Name: "parent", Roms: []dat.Rom{{Name: "a.bin", Size: int64(len(data)), CRC: crc}}},
	}
	childZip := filepath.Join(dir, "child.zip")
	if err := createZipFile(childZip, map[string][]byte{"a.bin": data}); err != nil {
		t.Fatalf("create child zip: %v", err)
	}
	cmd := &RomTestCommand{}
	nameToPath := map[string]string{"child": childZip}
	if issues, parent, skipped, mismatch, parentMissing := cmd.validateFile(defs, nameToPath, childZip); len(issues) != 0 || parent == "" || !parentMissing || skipped || mismatch {
		t.Fatalf("expected parent missing success with tag, got %v (parent %s, skipped %v, mismatch %v, parentMissing %v)", issues, parent, skipped, mismatch, parentMissing)
	}

	parentZip := filepath.Join(dir, "parent.zip")
	if err := createZipFile(parentZip, map[string][]byte{"a.bin": data}); err != nil {
		t.Fatalf("create parent zip: %v", err)
	}
	nameToPath["parent"] = parentZip
	if issues, parent, skipped, mismatch, parentMissing := cmd.validateFile(defs, nameToPath, childZip); len(issues) != 0 || parent == "" || skipped || mismatch || parentMissing {
		t.Fatalf("expected no issues when parent present, got %v (parent %s, skipped %v, mismatch %v, parentMissing %v)", issues, parent, skipped, mismatch, parentMissing)
	}
}

func createZipFile(path string, files map[string][]byte) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	w := zip.NewWriter(out)
	for name, data := range files {
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return nil
}
