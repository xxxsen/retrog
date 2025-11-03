package app

// Meta is the root structure written to the meta json file.
type Meta struct {
	Category []Category `json:"category"`
}

// Category groups games under the same collection.
type Category struct {
	CatName    string `json:"cat_name"`
	Collection string `json:"collection,omitempty"`
	GameList   []Game `json:"game_list"`
}

// Game describes a single ROM entry in the exported meta file.
type Game struct {
	Name  string `json:"name"`
	Files []File `json:"files"`
	Desc  string `json:"desc"`
	Media Media  `json:"media"`
}

// File holds uploaded ROM information.
type File struct {
	Hash     string `json:"hash"`
	Ext      string `json:"ext"`
	Size     int64  `json:"size"`
	FileName string `json:"file_name"`
}

// Media contains optional media assets.
type Media struct {
	BoxFront   string `json:"boxfront,omitempty"`
	Boxart     string `json:"boxart,omitempty"`
	Screenshot string `json:"screenshot,omitempty"`
	Video      string `json:"video,omitempty"`
	Logo       string `json:"logo,omitempty"`
}
