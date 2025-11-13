package model

type UnlinkFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

type UnlinkLocation struct {
	Location string       `json:"location"`
	Count    int          `json:"count"`
	Total    int          `json:"total"`
	Files    []UnlinkFile `json:"files"`
}

type UnlinkReport struct {
	Count  int              `json:"count"`
	Unlink []UnlinkLocation `json:"unlink"`
}

type MatchResult struct {
	Location   string       `json:"location"`
	MissCount  int          `json:"miss-count"`
	MatchCount int          `json:"match-count"`
	Files      []UnlinkFile `json:"files"`
}

type MatchOutput struct {
	Match []MatchResult `json:"match"`
}

type MediaDir struct {
	Dir   string   `json:"dir"`
	Count int      `json:"count"`
	Files []string `json:"files"`
}

type MediaLocation struct {
	Location string     `json:"location"`
	Dirs     []MediaDir `json:"dirs"`
}

type MediaReport struct {
	UnlinkMedia []MediaLocation `json:"unlink-media"`
}

type MatchedMeta struct {
	Entry Entry
	ID    int64
}
