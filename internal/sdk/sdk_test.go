package sdk

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// stdCtx implements sdk.Context for tests.
type stdCtx struct {
	context.Context
}

func (s stdCtx) Done() <-chan struct{} { return s.Context.Done() }
func (s stdCtx) Err() error            { return s.Context.Err() }

// helper to create a DAT file for fbneo.
const fbneoSampleDat = `<?xml version="1.0"?>
<datafile>
  <header><name>fbneo</name></header>
  <game name="testgame" romof="neogeo">
    <description>Test Game</description>
    <rom name="a.bin" size="3" crc="616263"/>
  </game>
  <game name="neogeo">
    <description>BIOS</description>
    <rom name="sfix.sfix" size="2" crc=""/>
  </game>
</datafile>`

const mameSampleDat = `<?xml version="1.0"?>
<datafile>
  <header><name>MAME</name></header>
  <machine name="mamegame" romof="biosset">
    <description>Mame Game</description>
    <rom name="m.bin" size="3" crc="616263"/>
  </machine>
  <machine name="biosset">
    <description>BIOS</description>
    <rom name="bios.bin" size="2" crc=""/>
  </machine>
</datafile>`

func TestFBNeoSDK(t *testing.T) {
	dir := t.TempDir()
	datPath := filepath.Join(dir, "fbneo.dat")
	if err := os.WriteFile(datPath, []byte(fbneoSampleDat), 0o644); err != nil {
		t.Fatalf("write dat: %v", err)
	}
	romDir := filepath.Join(dir, "roms")
	biosDir := filepath.Join(dir, "bios")
	if err := os.MkdirAll(romDir, 0o755); err != nil {
		t.Fatalf("mkdir roms: %v", err)
	}
	if err := os.MkdirAll(biosDir, 0o755); err != nil {
		t.Fatalf("mkdir bios: %v", err)
	}

	writeZip(t, filepath.Join(romDir, "testgame.zip"), map[string][]byte{
		"a.bin": []byte("abc"),
	})
	writeZip(t, filepath.Join(biosDir, "neogeo.zip"), map[string][]byte{
		"sfix.sfix": []byte{0x4b, 0x2e},
	})

	sdk, err := NewFBNeoTestSDK(datPath)
	if err != nil {
		t.Fatalf("init sdk: %v", err)
	}
	res, err := sdk.TestDir(stdCtx{context.Background()}, romDir, biosDir, []string{"zip"})
	if err != nil {
		t.Fatalf("test dir: %v", err)
	}
	if len(res.List) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.List))
	}
	r := res.List[0]
	if len(r.RedSubRomResultList) != 0 {
		t.Fatalf("expected no red, got red %d yellow %d", len(r.RedSubRomResultList), len(r.YellowSubRomResultList))
	}
}

func TestMameSDK(t *testing.T) {
	dir := t.TempDir()
	datPath := filepath.Join(dir, "mame.dat")
	if err := os.WriteFile(datPath, []byte(mameSampleDat), 0o644); err != nil {
		t.Fatalf("write dat: %v", err)
	}
	romDir := filepath.Join(dir, "roms")
	biosDir := filepath.Join(dir, "bios")
	if err := os.MkdirAll(romDir, 0o755); err != nil {
		t.Fatalf("mkdir roms: %v", err)
	}
	if err := os.MkdirAll(biosDir, 0o755); err != nil {
		t.Fatalf("mkdir bios: %v", err)
	}

	writeZip(t, filepath.Join(romDir, "mamegame.zip"), map[string][]byte{
		"m.bin": []byte("abc"),
	})
	writeZip(t, filepath.Join(biosDir, "biosset.zip"), map[string][]byte{
		"bios.bin": []byte{0x4b, 0x2e},
	})

	sdk, err := NewMameTestSDK(datPath)
	if err != nil {
		t.Fatalf("init sdk: %v", err)
	}
	res, err := sdk.TestDir(stdCtx{context.Background()}, romDir, biosDir, []string{"zip"})
	if err != nil {
		t.Fatalf("test dir: %v", err)
	}
	if len(res.List) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.List))
	}
	r := res.List[0]
	if len(r.RedSubRomResultList) != 0 {
		t.Fatalf("expected no red, got red %d yellow %d", len(r.RedSubRomResultList), len(r.YellowSubRomResultList))
	}
}

func TestParentAndBiosLabeling(t *testing.T) {
	dir := t.TempDir()
	datPath := filepath.Join(dir, "chain.dat")
	datContent := `<?xml version="1.0"?>
<datafile>
  <header><name>Chain</name></header>
  <game name="child" romof="parent1">
    <rom name="c.bin" size="1" crc="00"/>
  </game>
  <game name="parent1" romof="biosset">
    <rom name="p.bin" size="1" crc="00"/>
  </game>
  <game name="biosset">
    <rom name="b.bin" size="1" crc=""/>
  </game>
</datafile>`
	if err := os.WriteFile(datPath, []byte(datContent), 0o644); err != nil {
		t.Fatalf("write dat: %v", err)
	}
	romDir := filepath.Join(dir, "roms")
	biosDir := filepath.Join(dir, "bios")
	if err := os.MkdirAll(romDir, 0o755); err != nil {
		t.Fatalf("mkdir roms: %v", err)
	}
	if err := os.MkdirAll(biosDir, 0o755); err != nil {
		t.Fatalf("mkdir bios: %v", err)
	}
	writeZip(t, filepath.Join(romDir, "child.zip"), map[string][]byte{"c.bin": {0}})
	writeZip(t, filepath.Join(romDir, "parent1.zip"), map[string][]byte{"p.bin": {0}})
	writeZip(t, filepath.Join(biosDir, "biosset.zip"), map[string][]byte{"b.bin": {0}})

	sdk, err := NewFBNeoTestSDK(datPath)
	if err != nil {
		t.Fatalf("init sdk: %v", err)
	}
	res, err := sdk.TestDir(stdCtx{context.Background()}, romDir, biosDir, []string{"zip"})
	if err != nil {
		t.Fatalf("test dir: %v", err)
	}
	var child *RomFileTestResult
	for _, r := range res.List {
		if r.RomName == "child" {
			child = r
			break
		}
	}
	if child == nil {
		t.Fatalf("child result not found")
	}
	parents := child.ParentList
	if len(parents) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(parents))
	}
	if parents[0].IsBios || !parents[0].Exist {
		t.Fatalf("parent1 flags incorrect: %+v", parents[0])
	}
	if !parents[1].IsBios || !parents[1].Exist {
		t.Fatalf("biosset flags incorrect: %+v", parents[1])
	}
}

func writeZip(t *testing.T, path string, files map[string][]byte) {
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
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
}
