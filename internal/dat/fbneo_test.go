package dat

import (
	"strings"
	"testing"
)

const sampleDat = `<?xml version="1.0"?>
<!DOCTYPE datafile PUBLIC "-//FinalBurn Neo//DTD ROM Management Datafile//EN" "http://www.logiqx.com/Dats/datafile.dtd">
<datafile>
	<header>
		<name>FinalBurn Neo - Arcade Games</name>
		<description>FinalBurn Neo v1.0.0.03 Arcade Games</description>
		<category>Standard DatFile</category>
		<version>1.0.0.03</version>
		<author>FinalBurn Neo</author>
		<homepage>https://neo-source.com/</homepage>
		<url>https://neo-source.com/</url>
		<clrmamepro forcenodump="ignore"/>
	</header>
	<game isbios="yes" name="testgame" sourcefile="driver/test.cpp">
		<comment>BIOS only</comment>
		<description>Test Game</description>
		<year>1991</year>
		<manufacturer>Codex</manufacturer>
		<video type="raster" orientation="horizontal" width="320" height="240" aspectx="4" aspecty="3"/>
		<driver status="good"/>
		<rom name="test.rom" size="1024" crc="abcd1234" md5="b1946ac92492d2347c6235b4d2611184" sha1="2aae6c35c94fcfb415dbe95f408b9ce91ee846ed" merge="base"/>
		<sample name="test.wav"/>
	</game>
</datafile>`

func TestParserParse(t *testing.T) {
	parser := NewParser()
	df, err := parser.Parse(strings.NewReader(sampleDat))
	if err != nil {
		t.Fatalf("expected parser to succeed, got error: %v", err)
	}

	if df.Header.Name != "FinalBurn Neo - Arcade Games" {
		t.Fatalf("unexpected header name: %s", df.Header.Name)
	}

	game := df.FindGame("testgame")
	if game == nil {
		t.Fatalf("expected to find game testgame")
	}
	if game.SourceFile != "driver/test.cpp" || game.IsBios != "yes" {
		t.Fatalf("unexpected game attrs: %+v", game)
	}
	if game.Comment != "BIOS only" || game.Description != "Test Game" {
		t.Fatalf("unexpected game fields: %+v", game)
	}

	if game.Video == nil || game.Video.Width != 320 || game.Video.Height != 240 || game.Video.Orientation != "horizontal" {
		t.Fatalf("unexpected video block: %+v", game.Video)
	}
	if game.Driver == nil || game.Driver.Status != "good" {
		t.Fatalf("unexpected driver block: %+v", game.Driver)
	}

	if len(game.Roms) != 1 {
		t.Fatalf("expected one rom, got %d", len(game.Roms))
	}
	if game.Roms[0].Size != 1024 || game.Roms[0].CRC != "abcd1234" || game.Roms[0].Merge != "base" {
		t.Fatalf("unexpected rom entry: %+v", game.Roms[0])
	}

	if len(game.Samples) != 1 || game.Samples[0].Name != "test.wav" {
		t.Fatalf("unexpected sample entry: %+v", game.Samples)
	}
}
