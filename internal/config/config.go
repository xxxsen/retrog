package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Config describes the application level configuration loaded from json.
type Config struct {
	S3 S3Config `json:"s3"`
}

// S3Config holds the options for accessing the object store.
type S3Config struct {
	Host            string `json:"host"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	ForcePathStyle  bool   `json:"force_path_style"`
}

// LoadFirst tries to load configuration from the given paths, returning the
// first successfully decoded configuration. If none of the paths contain a
// readable config, an error is returned.
func LoadFirst(paths ...string) (*Config, error) {
	var lastErr error
	for _, path := range paths {
		if path == "" {
			continue
		}
		cfg, err := Load(path)
		if errors.Is(err, os.ErrNotExist) {
			lastErr = err
			continue
		}
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("config not found in paths: %v", paths)
	}
	return nil, lastErr
}

// Load reads configuration from a single json file path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate performs basic validation of the configuration.
func (c *Config) Validate() error {
	if c.S3.Host == "" {
		return errors.New("config.s3.host must be set")
	}
	if c.S3.Bucket == "" {
		return errors.New("config.s3.bucket must be set")
	}
	return nil
}
