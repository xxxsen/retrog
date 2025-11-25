package dat

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

// MameParser reads MAME-style DAT files.
type MameParser struct{}

// NewMameParser builds a fresh MAME DAT parser.
func NewMameParser() MameParser {
	return MameParser{}
}

// ParseFile opens and parses a MAME DAT file.
func (p MameParser) ParseFile(path string) (*MameDataFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mame dat %s: %w", path, err)
	}
	defer f.Close()
	return p.Parse(f)
}

// Parse consumes MAME DAT XML content from the provided reader.
func (p MameParser) Parse(r io.Reader) (*MameDataFile, error) {
	decoder := xml.NewDecoder(r)
	decoder.Strict = false // DTD is referenced; relax strict parsing.

	var df MameDataFile
	if err := decoder.Decode(&df); err != nil {
		return nil, fmt.Errorf("decode mame dat: %w", err)
	}
	return &df, nil
}

// MameDataFile is the root node of a MAME DAT file.
type MameDataFile struct {
	XMLName  xml.Name      `xml:"datafile"`
	Header   MameHeader    `xml:"header"`
	Machines []MameMachine `xml:"machine"`
}

// MameHeader carries top-level metadata for the DAT.
type MameHeader struct {
	Name        string     `xml:"name"`
	Description string     `xml:"description"`
	Category    string     `xml:"category"`
	Version     string     `xml:"version"`
	Date        string     `xml:"date"`
	Author      string     `xml:"author"`
	Email       string     `xml:"email"`
	Homepage    string     `xml:"homepage"`
	URL         string     `xml:"url"`
	ClrMamePro  ClrMamePro `xml:"clrmamepro"`
}

// MameMachine represents a single machine entry.
type MameMachine struct {
	Name         string                `xml:"name,attr"`
	SourceFile   string                `xml:"sourcefile,attr,omitempty"`
	CloneOf      string                `xml:"cloneof,attr,omitempty"`
	RomOf        string                `xml:"romof,attr,omitempty"`
	SampleOf     string                `xml:"sampleof,attr,omitempty"`
	IsBios       string                `xml:"isbios,attr,omitempty"`
	IsDevice     string                `xml:"isdevice,attr,omitempty"`
	IsMechanical string                `xml:"ismechanical,attr,omitempty"`
	Runnable     string                `xml:"runnable,attr,omitempty"`
	Description  string                `xml:"description"`
	Year         string                `xml:"year"`
	Manufacturer string                `xml:"manufacturer"`
	BiosSets     []MameBiosSet         `xml:"biosset"`
	Roms         []Rom                 `xml:"rom"`
	Disks        []MameDisk            `xml:"disk"`
	Samples      []Sample              `xml:"sample"`
	DeviceRefs   []MameDeviceReference `xml:"device_ref"`
	Driver       *Driver               `xml:"driver"`
	SoftwareList []MameSoftwareList    `xml:"softwarelist"`
}

// MameBiosSet captures BIOS options for a machine.
type MameBiosSet struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"description,attr"`
	Default     string `xml:"default,attr,omitempty"`
}

// MameDisk describes CHD/disk entries.
type MameDisk struct {
	Name   string `xml:"name,attr"`
	Merge  string `xml:"merge,attr,omitempty"`
	SHA1   string `xml:"sha1,attr,omitempty"`
	Region string `xml:"region,attr,omitempty"`
	Status string `xml:"status,attr,omitempty"`
}

// MameDeviceReference links to a referenced device.
type MameDeviceReference struct {
	Name string `xml:"name,attr"`
}

// MameSoftwareList captures an associated software list entry.
type MameSoftwareList struct {
	Name   string `xml:"name,attr"`
	Status string `xml:"status,attr,omitempty"`
	Filter string `xml:"filter,attr,omitempty"`
}

// FindMachine returns the first machine matching the given name.
func (df *MameDataFile) FindMachine(name string) *MameMachine {
	if df == nil {
		return nil
	}
	for i := range df.Machines {
		if df.Machines[i].Name == name {
			return &df.Machines[i]
		}
	}
	return nil
}
