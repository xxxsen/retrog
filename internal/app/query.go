package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/model"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// QueryCommand filters meta entries by ROM hash and prints them as JSON.
type QueryCommand struct {
	hashList string

	hashes []string
}

func (c *QueryCommand) Name() string { return "query" }

func (c *QueryCommand) Desc() string {
	return "根据 ROM 哈希查询元数据并输出 JSON"
}

func NewQueryCommand() *QueryCommand { return &QueryCommand{} }

func (c *QueryCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.hashList, "hash", "", "逗号分隔的 ROM 哈希列表")
}

func (c *QueryCommand) PreRun(ctx context.Context) error {
	c.hashes = c.hashes[:0]
	if strings.TrimSpace(c.hashList) == "" {
		return errors.New("query requires --hash")
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
		zap.Strings("hashes", c.hashes),
	)
	return nil
}

func (c *QueryCommand) Run(ctx context.Context) error {
	result := make(map[string]model.Entry)
	logger := logutil.GetLogger(ctx)

	dao := appdb.MetaDao
	entries, missing, err := dao.FetchByHashes(ctx, c.hashes)
	if err != nil {
		return err
	}
	for _, hash := range missing {
		logger.Warn("hash not found in meta", zap.String("hash", hash))
	}
	for hash, entry := range entries {
		result[hash] = entry
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal query result: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func (c *QueryCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("query completed")
	return nil
}

func init() {
	RegisterRunner("query", func() IRunner { return NewQueryCommand() })
}
