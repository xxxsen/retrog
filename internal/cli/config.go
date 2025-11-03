package cli

import (
	"context"
	"os"
	"path/filepath"

	"retrog/internal/config"
	"retrog/internal/storage"

	"github.com/spf13/cobra"
)

const (
	// ConfigFlag is the CLI flag name used to specify an explicit config path.
	ConfigFlag = "config"

	defaultConfigName = "config.json"
	systemConfigPath  = "/etc/retrog.json"
)

type contextKey string

const cfgContextKey contextKey = "retrog/config"

func loadConfig(explicit string) (*config.Config, error) {
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

func configFromContext(cmd *cobra.Command) (*config.Config, bool) {
	if ctx := cmd.Context(); ctx != nil {
		if cfg, ok := ctx.Value(cfgContextKey).(*config.Config); ok {
			return cfg, true
		}
	}
	if root := cmd.Root(); root != cmd {
		if ctx := root.Context(); ctx != nil {
			if cfg, ok := ctx.Value(cfgContextKey).(*config.Config); ok {
				return cfg, true
			}
		}
	}
	return nil, false
}

func ensureConfig(cmd *cobra.Command) (*config.Config, error) {
	if cfg, ok := configFromContext(cmd); ok {
		return cfg, nil
	}

	cfgPath, _ := cmd.Root().PersistentFlags().GetString(ConfigFlag)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return nil, err
	}

	setConfigContext(cmd.Root(), cfg)
	if cmd != cmd.Root() {
		setConfigContext(cmd, cfg)
	}

	ctx := commandContext(cmd)
	storage.SetDefaultS3Config(cfg.S3)
	if _, err := storage.EnsureDefaultClient(ctx); err != nil {
		return nil, err
	}

	return cfg, nil
}

func getConfig(cmd *cobra.Command) (*config.Config, error) {
	if cfg, ok := configFromContext(cmd); ok {
		return cfg, nil
	}
	return ensureConfig(cmd)
}

func setConfigContext(cmd *cobra.Command, cfg *config.Config) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, cfgContextKey, cfg))
}
