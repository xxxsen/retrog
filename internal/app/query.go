package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// QueryCommand filters meta entries by ROM hash and prints them as JSON.
type QueryCommand struct {
	metaPath string
	hashList string

	hashes []string
}

func (c *QueryCommand) Name() string { return "query" }

func (c *QueryCommand) Desc() string {
	return "根据 ROM 哈希查询元数据并输出 JSON"
}

func NewQueryCommand() *QueryCommand { return &QueryCommand{} }

func (c *QueryCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.metaPath, "meta", "", "upload 命令生成的 meta.json")
	f.StringVar(&c.hashList, "hash", "", "逗号分隔的 ROM 哈希列表")
}

func (c *QueryCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.metaPath) == "" || strings.TrimSpace(c.hashList) == "" {
		return errors.New("query requires --meta and --hash")
	}
	for _, h := range strings.Split(c.hashList, ",") {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			c.hashes = append(c.hashes, trimmed)
		}
	}
	if len(c.hashes) == 0 {
		return errors.New("no valid hashes provided")
	}

	logutil.GetLogger(ctx).Info("starting query",
		zap.String("meta", c.metaPath),
		zap.Strings("hashes", c.hashes),
	)
	return nil
}

func (c *QueryCommand) Run(ctx context.Context) error {
	meta, err := c.loadMeta(c.metaPath)
	if err != nil {
		return err
	}

	result := make(Meta)
	logger := logutil.GetLogger(ctx)

	for _, hash := range c.hashes {
		entry, ok := meta[hash]
		if !ok {
			logger.Warn("hash not found in meta", zap.String("hash", hash))
			continue
		}
		result[hash] = entry
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal query result: %w", err)
	}

	if _, err := os.Stdout.Write(data); err != nil {
		return fmt.Errorf("write query result: %w", err)
	}
	_, _ = os.Stdout.WriteString("\n")

	return nil
}

func (c *QueryCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("query completed")
	return nil
}

func (c *QueryCommand) loadMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta file %s: %w", path, err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta json %s: %w", path, err)
	}
	return meta, nil
}

func init() {
	RegisterRunner("query", func() IRunner { return NewQueryCommand() })
}
