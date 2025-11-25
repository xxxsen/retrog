package dat

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

// Parser reads FinalBurn Neo DAT files.
type Parser struct{}

// NewParser builds a fresh DAT parser.
func NewParser() Parser {
	return Parser{}
}

// ParseFile opens and parses a FinalBurn Neo DAT file.
func (p Parser) ParseFile(path string) (*DataFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open fbneo dat %s: %w", path, err)
	}
	defer f.Close()
	return p.Parse(f)
}

// Parse consumes DAT XML content from the provided reader.
func (p Parser) Parse(r io.Reader) (*DataFile, error) {
	decoder := xml.NewDecoder(r)
	decoder.Strict = false // fbneo.dat includes a DTD; relax strict parsing.

	var df DataFile
	if err := decoder.Decode(&df); err != nil {
		return nil, fmt.Errorf("decode fbneo dat: %w", err)
	}
	return &df, nil
}

// DataFile is the root node of a FinalBurn Neo DAT file.
type DataFile struct {
	XMLName xml.Name `xml:"datafile"`
	Header  Header   `xml:"header"`
	Games   []Game   `xml:"game"`
}

// Header carries top-level metadata for the DAT.
type Header struct {
	Name        string     `xml:"name"`
	Description string     `xml:"description"`
	Category    string     `xml:"category"`
	Version     string     `xml:"version"`
	Author      string     `xml:"author"`
	Homepage    string     `xml:"homepage"`
	URL         string     `xml:"url"`
	ClrMamePro  ClrMamePro `xml:"clrmamepro"`
}

// ClrMamePro stores clrmamepro options.
type ClrMamePro struct {
	ForceNoDump string `xml:"forcenodump,attr,omitempty"`
}

// Game represents a single ROM set entry.
type Game struct {
	Name         string   `xml:"name,attr"`
	SourceFile   string   `xml:"sourcefile,attr,omitempty"`
	IsBios       string   `xml:"isbios,attr,omitempty"`
	CloneOf      string   `xml:"cloneof,attr,omitempty"`
	RomOf        string   `xml:"romof,attr,omitempty"`
	Description  string   `xml:"description"`
	Comment      string   `xml:"comment"`
	Year         string   `xml:"year"`
	Manufacturer string   `xml:"manufacturer"`
	Video        *Video   `xml:"video"`
	Driver       *Driver  `xml:"driver"`
	Roms         []Rom    `xml:"rom"`
	Samples      []Sample `xml:"sample"`
}

// Video captures display information for the game.
type Video struct {
	Type        string `xml:"type,attr,omitempty"`
	Orientation string `xml:"orientation,attr,omitempty"`
	Width       int    `xml:"width,attr,omitempty"`
	Height      int    `xml:"height,attr,omitempty"`
	AspectX     int    `xml:"aspectx,attr,omitempty"`
	AspectY     int    `xml:"aspecty,attr,omitempty"`
}

// Driver holds driver status info.
type Driver struct {
	Status string `xml:"status,attr,omitempty"`
}

// Rom describes a single ROM file entry.
type Rom struct {
	Name   string `xml:"name,attr"`
	Size   int64  `xml:"size,attr,omitempty"`
	CRC    string `xml:"crc,attr,omitempty"`
	MD5    string `xml:"md5,attr,omitempty"`
	SHA1   string `xml:"sha1,attr,omitempty"`
	Merge  string `xml:"merge,attr,omitempty"`
	Status string `xml:"status,attr,omitempty"`
}

// Sample represents an external sample file used by the ROM.
type Sample struct {
	Name string `xml:"name,attr"`
}

// FindGame returns the first game matching the given name.
func (df *DataFile) FindGame(name string) *Game {
	if df == nil {
		return nil
	}
	for i := range df.Games {
		if df.Games[i].Name == name {
			return &df.Games[i]
		}
	}
	return nil
}
