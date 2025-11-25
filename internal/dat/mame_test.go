package dat

import (
	"strings"
	"testing"
)

const sampleMameDat = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE datafile PUBLIC "-//Logiqx//DTD ROM Management Datafile//EN" "http://www.logiqx.com/Dats/datafile.dtd">
<datafile>
  <header>
    <name>MAME</name>
    <description>MAME Arcade 0.282 (Oct 31 2025)</description>
    <category>Emulation</category>
    <version>0.282</version>
    <date>31/10/2025</date>
    <author>AntoPISA</author>
    <email>progettosnaps@gmail.com</email>
    <homepage>https://www.progettosnaps.net/</homepage>
    <url>https://www.progettosnaps.net/dats/MAME/</url>
    <clrmamepro/>
  </header>
  <machine name="mame-test" sourcefile="driver/test.cpp" cloneof="base" romof="base" sampleof="base" isbios="no" isdevice="no" ismechanical="no" runnable="yes">
    <description>Test Machine</description>
    <year>2025</year>
    <manufacturer>Codex</manufacturer>
    <biosset name="bios1" description="BIOS 1" default="yes"/>
    <biosset name="bios2" description="BIOS 2"/>
    <rom name="game.rom" size="2048" crc="abcd1234" md5="b1946ac92492d2347c6235b4d2611184" sha1="2aae6c35c94fcfb415dbe95f408b9ce91ee846ed" status="good"/>
    <disk name="game.chd" sha1="f00dbabe" merge="base.chd" region="maincpu" status="baddump"/>
    <sample name="test.wav"/>
    <device_ref name="z80"/>
    <driver status="good"/>
    <softwarelist name="nes" status="original" filter="mario"/>
  </machine>
</datafile>`

func TestMameParserParse(t *testing.T) {
	parser := NewMameParser()
	df, err := parser.Parse(strings.NewReader(sampleMameDat))
	if err != nil {
		t.Fatalf("expected parser to succeed, got error: %v", err)
	}

	if df.Header.Name != "MAME" || df.Header.Version != "0.282" {
		t.Fatalf("unexpected header: %+v", df.Header)
	}
	if len(df.Machines) != 1 {
		t.Fatalf("expected 1 machine, got %d", len(df.Machines))
	}

	machine := df.FindMachine("mame-test")
	if machine == nil {
		t.Fatalf("expected to find machine mame-test")
	}
	if machine.Description != "Test Machine" || machine.Manufacturer != "Codex" {
		t.Fatalf("unexpected machine fields: %+v", machine)
	}
	if machine.Driver == nil || machine.Driver.Status != "good" {
		t.Fatalf("unexpected driver: %+v", machine.Driver)
	}
	if len(machine.BiosSets) != 2 || machine.BiosSets[0].Default != "yes" {
		t.Fatalf("unexpected bios sets: %+v", machine.BiosSets)
	}
	if len(machine.Roms) != 1 || machine.Roms[0].Name != "game.rom" || machine.Roms[0].Status != "good" {
		t.Fatalf("unexpected rom: %+v", machine.Roms)
	}
	if len(machine.Disks) != 1 || machine.Disks[0].Region != "maincpu" || machine.Disks[0].Status != "baddump" {
		t.Fatalf("unexpected disks: %+v", machine.Disks)
	}
	if len(machine.Samples) != 1 || machine.Samples[0].Name != "test.wav" {
		t.Fatalf("unexpected samples: %+v", machine.Samples)
	}
	if len(machine.DeviceRefs) != 1 || machine.DeviceRefs[0].Name != "z80" {
		t.Fatalf("unexpected device refs: %+v", machine.DeviceRefs)
	}
	if len(machine.SoftwareList) != 1 || machine.SoftwareList[0].Name != "nes" || machine.SoftwareList[0].Filter != "mario" {
		t.Fatalf("unexpected software list: %+v", machine.SoftwareList)
	}
}
