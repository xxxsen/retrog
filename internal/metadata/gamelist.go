package metadata

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

type GamelistDocument struct {
	Games []GamelistEntry `xml:"game"`
}

type GamelistEntry struct {
	Path        string   `xml:"path"`
	Name        string   `xml:"name"`
	Description string   `xml:"desc"`
	Image       string   `xml:"image"`
	Thumbnail   string   `xml:"thumbnail"`
	Marquee     string   `xml:"marquee"`
	Video       string   `xml:"video"`
	Developer   string   `xml:"developer"`
	Publisher   string   `xml:"publisher"`
	Genres      []string `xml:"genre"`
	ReleaseDate string   `xml:"releasedate"`
}

func ParseGamelistFile(path string) (*GamelistDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open gamelist %s: %w", path, err)
	}
	defer f.Close()

	var doc struct {
		Games []GamelistEntry `xml:"game"`
	}
	decoder := xml.NewDecoder(f)
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode gamelist %s: %w", path, err)
	}

	for i := range doc.Games {
		doc.Games[i].Path = strings.TrimSpace(doc.Games[i].Path)
		doc.Games[i].Name = strings.TrimSpace(doc.Games[i].Name)
		doc.Games[i].Description = strings.TrimSpace(doc.Games[i].Description)
		doc.Games[i].Image = strings.TrimSpace(doc.Games[i].Image)
		doc.Games[i].Thumbnail = strings.TrimSpace(doc.Games[i].Thumbnail)
		doc.Games[i].Marquee = strings.TrimSpace(doc.Games[i].Marquee)
		doc.Games[i].Video = strings.TrimSpace(doc.Games[i].Video)
		doc.Games[i].Developer = strings.TrimSpace(doc.Games[i].Developer)
		doc.Games[i].Publisher = strings.TrimSpace(doc.Games[i].Publisher)
		doc.Games[i].ReleaseDate = strings.TrimSpace(doc.Games[i].ReleaseDate)
		for j := range doc.Games[i].Genres {
			doc.Games[i].Genres[j] = strings.TrimSpace(doc.Games[i].Genres[j])
		}
	}

	return &GamelistDocument{Games: doc.Games}, nil
}
