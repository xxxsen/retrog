package app

// Meta stores ROM metadata keyed by ROM hash.
type Meta map[string]MetaEntry

// MetaEntry describes a ROM's metadata and associated media.
type MetaEntry struct {
	Name  string            `json:"name"`
	Desc  string            `json:"desc"`
	Media map[string]string `json:"media,omitempty"`
}
