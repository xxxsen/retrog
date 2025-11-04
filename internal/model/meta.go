package model

import (
	"encoding/json"
	"strings"
)

// Entry represents a ROM metadata record stored in the database.
type Entry struct {
	Name      string       `json:"name"`
	Desc      string       `json:"desc"`
	Size      int64        `json:"size"`
	Media     []MediaEntry `json:"media,omitempty"`
	Developer string       `json:"developer,omitempty"`
	Publisher string       `json:"publisher,omitempty"`
	Genres    []string     `json:"genres,omitempty"`
	ReleaseAt int64        `json:"release_at,omitempty"`
}

// MediaEntry captures a single media asset description.
type MediaEntry struct {
	Type string `json:"type"`
	Hash string `json:"hash"`
	Ext  string `json:"ext"`
	Size int64  `json:"size"`
}

type extInfoPayload struct {
	Media     []MediaEntry `json:"media,omitempty"`
	Developer string       `json:"developer,omitempty"`
	Publisher string       `json:"publisher,omitempty"`
	Genres    []string     `json:"genres,omitempty"`
	ReleaseAt int64        `json:"release_at,omitempty"`
}

// MarshalExtInfo converts entry attachments into ext_info JSON.
func (e Entry) MarshalExtInfo() (string, error) {
	payload := extInfoPayload{
		Media:     e.Media,
		Developer: e.Developer,
		Publisher: e.Publisher,
		Genres:    e.Genres,
		ReleaseAt: e.ReleaseAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FromRecord rebuilds an Entry from raw fields and ext_info JSON.
func FromRecord(name, desc string, size int64, extInfo string) (Entry, error) {
	entry := Entry{Name: name, Desc: desc, Size: size}
	if strings.TrimSpace(extInfo) == "" {
		return entry, nil
	}
	var payload extInfoPayload
	if err := json.Unmarshal([]byte(extInfo), &payload); err != nil {
		return Entry{}, err
	}
	entry.Media = payload.Media
	entry.Developer = payload.Developer
	entry.Publisher = payload.Publisher
	entry.Genres = payload.Genres
	entry.ReleaseAt = payload.ReleaseAt
	return entry, nil
}
