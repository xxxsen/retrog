package metadata

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type GamelistDocument struct {
	Provider ProviderInfo     `xml:"provider"`
	Games    []GamelistEntry  `xml:"game"`
	Folders  []GamelistFolder `xml:"folder"`
}

// ProviderInfo describes metadata about the gamelist file creator/source.
type ProviderInfo struct {
	System   string
	Software string
	Database string
	Web      string
}

// ScrapInfo stores the attributes on the <scrap> tag.
type ScrapInfo struct {
	Name string `xml:"name,attr"`
	Date string `xml:"date,attr"`
}

type GamelistEntry struct {
	ID          string    `xml:"id,attr"`
	Source      string    `xml:"source,attr"`
	Path        string    `xml:"path"`
	Name        string    `xml:"name"`
	Description string    `xml:"desc"`
	Image       string    `xml:"image"`
	Thumbnail   string    `xml:"thumbnail"`
	Marquee     string    `xml:"marquee"`
	Video       string    `xml:"video"`
	Developer   string    `xml:"developer"`
	Publisher   string    `xml:"publisher"`
	Genres      []string  `xml:"genre"`
	ReleaseDate string    `xml:"releasedate"`
	Rating      string    `xml:"rating"`
	Scrap       ScrapInfo `xml:"scrap"`
	Players     string    `xml:"players"`
	Lang        string    `xml:"lang"`
	PlayCount   string    `xml:"playcount"`
	LastPlayed  string    `xml:"lastplayed"`
	Family      string    `xml:"family"`
	Region      string    `xml:"region"`
	SortName    string    `xml:"sortname"`
	Manual      string    `xml:"manual"`
	Hidden      bool      `xml:"hidden"`
	Hash        string    `xml:"hash"`
	MD5         string    `xml:"md5"`
	GenreID     string    `xml:"genreid"`
	CRC32       string    `xml:"crc32"`
	CheevosID   string    `xml:"cheevosId"`
	CheevosHash string    `xml:"cheevosHash"`
	Adult       bool      `xml:"adult"`
	Mix         string    `xml:"mix"`
}

type GamelistFolder struct {
	Path        string `xml:"path"`
	Image       string `xml:"image"`
	Name        string `xml:"name"`
	Description string `xml:"desc"`
	ReleaseDate string `xml:"releasedate"`
	Developer   string `xml:"developer"`
	Publisher   string `xml:"publisher"`
	Genre       string `xml:"genre"`
	Players     string `xml:"players"`
}

type providerXML struct {
	SystemUpper string `xml:"System"`
	SystemLower string `xml:"system"`
	Software    string `xml:"software"`
	Database    string `xml:"database"`
	Web         string `xml:"web"`
}

func ParseGamelistFile(path string) (*GamelistDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open gamelist %s: %w", path, err)
	}
	defer f.Close()

	var doc struct {
		Provider providerXML      `xml:"provider"`
		Games    []GamelistEntry  `xml:"game"`
		Folders  []GamelistFolder `xml:"folder"`
	}
	decoder := xml.NewDecoder(f)
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode gamelist %s: %w", path, err)
	}

	for i := range doc.Games {
		entry := &doc.Games[i]
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Source = strings.TrimSpace(entry.Source)
		entry.Path = strings.TrimSpace(entry.Path)
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Description = strings.TrimSpace(entry.Description)
		entry.Image = strings.TrimSpace(entry.Image)
		entry.Thumbnail = strings.TrimSpace(entry.Thumbnail)
		entry.Marquee = strings.TrimSpace(entry.Marquee)
		entry.Video = strings.TrimSpace(entry.Video)
		entry.Developer = strings.TrimSpace(entry.Developer)
		entry.Publisher = strings.TrimSpace(entry.Publisher)
		entry.ReleaseDate = strings.TrimSpace(entry.ReleaseDate)
		entry.Rating = strings.TrimSpace(entry.Rating)
		entry.Scrap.Name = strings.TrimSpace(entry.Scrap.Name)
		entry.Scrap.Date = strings.TrimSpace(entry.Scrap.Date)
		entry.Players = strings.TrimSpace(entry.Players)
		entry.Lang = strings.TrimSpace(entry.Lang)
		entry.PlayCount = strings.TrimSpace(entry.PlayCount)
		entry.LastPlayed = strings.TrimSpace(entry.LastPlayed)
		entry.Family = strings.TrimSpace(entry.Family)
		entry.Region = strings.TrimSpace(entry.Region)
		entry.SortName = strings.TrimSpace(entry.SortName)
		entry.Manual = strings.TrimSpace(entry.Manual)
		entry.Hash = strings.TrimSpace(entry.Hash)
		entry.MD5 = strings.TrimSpace(entry.MD5)
		entry.GenreID = strings.TrimSpace(entry.GenreID)
		entry.CRC32 = strings.TrimSpace(entry.CRC32)
		entry.CheevosID = strings.TrimSpace(entry.CheevosID)
		entry.CheevosHash = strings.TrimSpace(entry.CheevosHash)
		entry.Mix = strings.TrimSpace(entry.Mix)
		for j := range entry.Genres {
			entry.Genres[j] = strings.TrimSpace(entry.Genres[j])
		}
	}

	for i := range doc.Folders {
		folder := &doc.Folders[i]
		folder.Path = strings.TrimSpace(folder.Path)
		folder.Image = strings.TrimSpace(folder.Image)
		folder.Name = strings.TrimSpace(folder.Name)
		folder.Description = strings.TrimSpace(folder.Description)
		folder.ReleaseDate = strings.TrimSpace(folder.ReleaseDate)
		folder.Developer = strings.TrimSpace(folder.Developer)
		folder.Publisher = strings.TrimSpace(folder.Publisher)
		folder.Genre = strings.TrimSpace(folder.Genre)
		folder.Players = strings.TrimSpace(folder.Players)
	}

	systemValue := strings.TrimSpace(doc.Provider.SystemUpper)
	if systemValue == "" {
		systemValue = strings.TrimSpace(doc.Provider.SystemLower)
	}
	systemValue = strings.ToLower(systemValue)
	provider := ProviderInfo{
		System:   systemValue,
		Software: strings.TrimSpace(doc.Provider.Software),
		Database: strings.TrimSpace(doc.Provider.Database),
		Web:      strings.TrimSpace(doc.Provider.Web),
	}

	return &GamelistDocument{
		Provider: provider,
		Games:    doc.Games,
		Folders:  doc.Folders,
	}, nil
}

// WriteGamelistFile serialises the gamelist document to the provided file path.
func WriteGamelistFile(path string, doc *GamelistDocument) error {
	if doc == nil {
		return fmt.Errorf("gamelist document is nil")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("invalid gamelist output path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ensure gamelist dir %s: %w", path, err)
	}

	output := gamelistOutput{
		XMLName: xml.Name{Local: "gameList"},
		Folders: make([]gamelistFolderOutputEntry, 0, len(doc.Folders)),
		Games:   make([]gamelistOutputEntry, 0, len(doc.Games)),
	}
	if provider := doc.providerOutput(); provider != nil {
		output.Provider = provider
	}
	for _, folder := range doc.Folders {
		output.Folders = append(output.Folders, newFolderOutputEntry(folder))
	}
	for _, game := range doc.Games {
		output.Games = append(output.Games, newOutputEntry(game))
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create gamelist %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(xml.Header); err != nil {
		return fmt.Errorf("write xml header: %w", err)
	}

	encoder := xml.NewEncoder(f)
	encoder.Indent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("encode gamelist xml: %w", err)
	}
	if err := encoder.Flush(); err != nil {
		return fmt.Errorf("flush gamelist xml: %w", err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		return fmt.Errorf("terminate gamelist xml: %w", err)
	}
	return nil
}

func (doc *GamelistDocument) providerOutput() *providerOutput {
	system := strings.TrimSpace(doc.Provider.System)
	software := strings.TrimSpace(doc.Provider.Software)
	database := strings.TrimSpace(doc.Provider.Database)
	web := strings.TrimSpace(doc.Provider.Web)
	if system == "" && software == "" && database == "" && web == "" {
		return nil
	}
	return &providerOutput{
		System:   system,
		Software: software,
		Database: database,
		Web:      web,
	}
}

func newOutputEntry(src GamelistEntry) gamelistOutputEntry {
	entry := gamelistOutputEntry{
		ID:          strings.TrimSpace(src.ID),
		Source:      strings.TrimSpace(src.Source),
		Path:        strings.TrimSpace(src.Path),
		Name:        strings.TrimSpace(src.Name),
		Description: strings.TrimSpace(src.Description),
		Image:       strings.TrimSpace(src.Image),
		Thumbnail:   strings.TrimSpace(src.Thumbnail),
		Marquee:     strings.TrimSpace(src.Marquee),
		Video:       strings.TrimSpace(src.Video),
		Developer:   strings.TrimSpace(src.Developer),
		Publisher:   strings.TrimSpace(src.Publisher),
		ReleaseDate: strings.TrimSpace(src.ReleaseDate),
		Rating:      strings.TrimSpace(src.Rating),
		Players:     strings.TrimSpace(src.Players),
		Lang:        strings.TrimSpace(src.Lang),
		PlayCount:   strings.TrimSpace(src.PlayCount),
		LastPlayed:  strings.TrimSpace(src.LastPlayed),
		Family:      strings.TrimSpace(src.Family),
		Region:      strings.TrimSpace(src.Region),
		SortName:    strings.TrimSpace(src.SortName),
		Manual:      strings.TrimSpace(src.Manual),
		Hash:        strings.TrimSpace(src.Hash),
		MD5:         strings.TrimSpace(src.MD5),
		GenreID:     strings.TrimSpace(src.GenreID),
		CRC32:       strings.TrimSpace(src.CRC32),
		CheevosID:   strings.TrimSpace(src.CheevosID),
		CheevosHash: strings.TrimSpace(src.CheevosHash),
		Mix:         strings.TrimSpace(src.Mix),
	}
	entry.Hidden = src.Hidden
	entry.Adult = src.Adult

	if trimmedScrap := trimScrap(src.Scrap); trimmedScrap != nil {
		entry.Scrap = trimmedScrap
	}

	if len(src.Genres) > 0 {
		var genres []string
		for _, g := range src.Genres {
			if trimmed := strings.TrimSpace(g); trimmed != "" {
				genres = append(genres, trimmed)
			}
		}
		if len(genres) > 0 {
			entry.Genres = genres
		}
	}
	return entry
}

func trimScrap(s ScrapInfo) *scrapOutput {
	name := strings.TrimSpace(s.Name)
	date := strings.TrimSpace(s.Date)
	if name == "" && date == "" {
		return nil
	}
	return &scrapOutput{
		Name: name,
		Date: date,
	}
}

type gamelistOutput struct {
	XMLName  xml.Name                    `xml:"gameList"`
	Provider *providerOutput             `xml:"provider,omitempty"`
	Folders  []gamelistFolderOutputEntry `xml:"folder,omitempty"`
	Games    []gamelistOutputEntry       `xml:"game"`
}

type providerOutput struct {
	System   string `xml:"system,omitempty"`
	Software string `xml:"software,omitempty"`
	Database string `xml:"database,omitempty"`
	Web      string `xml:"web,omitempty"`
}

type scrapOutput struct {
	Name string `xml:"name,attr,omitempty"`
	Date string `xml:"date,attr,omitempty"`
}

type gamelistOutputEntry struct {
	XMLName     xml.Name     `xml:"game"`
	ID          string       `xml:"id,attr,omitempty"`
	Source      string       `xml:"source,attr,omitempty"`
	Path        string       `xml:"path,omitempty"`
	Name        string       `xml:"name,omitempty"`
	Description string       `xml:"desc,omitempty"`
	Image       string       `xml:"image,omitempty"`
	Thumbnail   string       `xml:"thumbnail,omitempty"`
	Marquee     string       `xml:"marquee,omitempty"`
	Video       string       `xml:"video,omitempty"`
	Developer   string       `xml:"developer,omitempty"`
	Publisher   string       `xml:"publisher,omitempty"`
	Genres      []string     `xml:"genre,omitempty"`
	ReleaseDate string       `xml:"releasedate,omitempty"`
	Rating      string       `xml:"rating,omitempty"`
	Scrap       *scrapOutput `xml:"scrap,omitempty"`
	Players     string       `xml:"players,omitempty"`
	Lang        string       `xml:"lang,omitempty"`
	PlayCount   string       `xml:"playcount,omitempty"`
	LastPlayed  string       `xml:"lastplayed,omitempty"`
	Family      string       `xml:"family,omitempty"`
	Region      string       `xml:"region,omitempty"`
	SortName    string       `xml:"sortname,omitempty"`
	Manual      string       `xml:"manual,omitempty"`
	Hidden      bool         `xml:"hidden,omitempty"`
	Hash        string       `xml:"hash,omitempty"`
	MD5         string       `xml:"md5,omitempty"`
	GenreID     string       `xml:"genreid,omitempty"`
	CRC32       string       `xml:"crc32,omitempty"`
	CheevosID   string       `xml:"cheevosId,omitempty"`
	CheevosHash string       `xml:"cheevosHash,omitempty"`
	Adult       bool         `xml:"adult,omitempty"`
	Mix         string       `xml:"mix,omitempty"`
}

type gamelistFolderOutputEntry struct {
	XMLName     xml.Name `xml:"folder"`
	Path        string   `xml:"path,omitempty"`
	Image       string   `xml:"image,omitempty"`
	Name        string   `xml:"name,omitempty"`
	Description string   `xml:"desc,omitempty"`
	ReleaseDate string   `xml:"releasedate,omitempty"`
	Developer   string   `xml:"developer,omitempty"`
	Publisher   string   `xml:"publisher,omitempty"`
	Genre       string   `xml:"genre,omitempty"`
	Players     string   `xml:"players,omitempty"`
}

func newFolderOutputEntry(src GamelistFolder) gamelistFolderOutputEntry {
	return gamelistFolderOutputEntry{
		Path:        strings.TrimSpace(src.Path),
		Image:       strings.TrimSpace(src.Image),
		Name:        strings.TrimSpace(src.Name),
		Description: strings.TrimSpace(src.Description),
		ReleaseDate: strings.TrimSpace(src.ReleaseDate),
		Developer:   strings.TrimSpace(src.Developer),
		Publisher:   strings.TrimSpace(src.Publisher),
		Genre:       strings.TrimSpace(src.Genre),
		Players:     strings.TrimSpace(src.Players),
	}
}
