package sdk

import "strings"

// SubRomFileTestState expresses the match status of a single ROM entry.
type SubRomFileTestState int

const (
	SubRomStateGreen  SubRomFileTestState = iota // filename and CRC matched
	SubRomStateYellow                            // partial match (name or CRC)
	SubRomStateRed                               // no usable match
)

// SubRomFile describes a ROM entry from a DAT.
type SubRomFile struct {
	Name      string
	MergeName string
	Size      int64
	CRC       string
	Optional  bool
}

// NormalizedName returns the merge name when present, otherwise the raw name.
func (s SubRomFile) NormalizedName() string {
	if strings.TrimSpace(s.MergeName) != "" {
		return s.MergeName
	}
	return s.Name
}

// SubRomFileTestResult captures the validation outcome for one ROM entry.
type SubRomFileTestResult struct {
	SubRom      *SubRomFile
	TestState   SubRomFileTestState
	TestMessage string
}

// ParentInfo describes a romof/bios ancestor.
type ParentInfo struct {
	Name  string
	Exist bool
}

// RomFileTestResult aggregates validation outcomes for a single archive.
type RomFileTestResult struct {
	FilePath               string
	RomName                string
	ParentList             []ParentInfo // romof chain, closest first, top-most last
	GreenSubRomResultList  []*SubRomFileTestResult
	YellowSubRomResultList []*SubRomFileTestResult
	RedSubRomResultList    []*SubRomFileTestResult
}

// RomTestResult is the overall outcome for a run.
type RomTestResult struct {
	List []*RomFileTestResult
}

// IRomTestSDK provides ROM validation.
type IRomTestSDK interface {
	TestDir(ctx Context, romdir string, biosdir string, exts []string) (*RomTestResult, error)
}

// Context is a minimal subset of context.Context to avoid tight coupling.
type Context interface {
	Done() <-chan struct{}
	Err() error
}
