package app

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
