package app

import (
	"encoding/json"
	"strings"
)

// Meta stores ROM metadata keyed by ROM hash.
type Meta map[string]MetaEntry

// MetaEntry describes a ROM's metadata and associated media.
type MetaEntry struct {
	Name  string       `json:"name"`
	Desc  string       `json:"desc"`
	Size  int64        `json:"size"`
	Media []MediaEntry `json:"media,omitempty"`
}

// MediaEntry captures a single media asset description.
type MediaEntry struct {
	Type string `json:"type"`
	Hash string `json:"hash"`
	Ext  string `json:"ext"`
	Size int64  `json:"size"`
}

type metaExtInfo struct {
	Media []MediaEntry `json:"media,omitempty"`
}

// MarshalExtInfo converts meta entry attachments into ext_info JSON.
func (m MetaEntry) MarshalExtInfo() (string, error) {
	payload := metaExtInfo{Media: m.Media}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MetaEntryFromRecord rebuilds a MetaEntry from raw fields and ext_info JSON.
func MetaEntryFromRecord(name, desc string, size int64, extInfo string) (MetaEntry, error) {
	entry := MetaEntry{Name: name, Desc: desc, Size: size}
	if strings.TrimSpace(extInfo) == "" {
		return entry, nil
	}
	var payload metaExtInfo
	if err := json.Unmarshal([]byte(extInfo), &payload); err != nil {
		return MetaEntry{}, err
	}
	entry.Media = payload.Media
	return entry, nil
}
