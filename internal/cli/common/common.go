package common

import (
	"os"
	"path/filepath"

	"retrog/internal/config"
)

const (
	// ConfigFlag is the CLI flag name used to specify an explicit config path.
	ConfigFlag = "config"

	defaultConfigName = "config.json"
	systemConfigPath  = "/etc/retrog.json"
)

// LoadConfig resolves the configuration file respecting precedence rules.
func LoadConfig(explicit string) (*config.Config, error) {
	searchPaths := make([]string, 0, 3)
	if explicit != "" {
		searchPaths = append(searchPaths, explicit)
	}

	if wd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(wd, defaultConfigName))
	}

	searchPaths = append(searchPaths, systemConfigPath)

	return config.LoadFirst(searchPaths...)
}
