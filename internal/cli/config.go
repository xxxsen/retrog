package cli

import (
	"github.com/xxxsen/retrog/internal/config"
)

var defaultKeyList = []string{
	"./config.json",
	"/etc/config.json",
}

func LoadConfig(explicit string) (*config.Config, error) {
	keyLists := append([]string{explicit}, defaultKeyList...)
	return config.LoadFirst(keyLists...)
}
