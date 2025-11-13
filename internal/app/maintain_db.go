package app

import (
	"context"
	"fmt"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/model"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type MaintainDBCommand struct {
	dryRun bool
}

func NewMaintainDBCommand() *MaintainDBCommand {
	return &MaintainDBCommand{
		dryRun: true,
	}
}

func (c *MaintainDBCommand) Name() string { return "maintain-db" }

func (c *MaintainDBCommand) Desc() string {
	return "清理 meta.db 中不合法的 ROM 元数据"
}

func (c *MaintainDBCommand) Init(f *pflag.FlagSet) {
	f.BoolVar(&c.dryRun, "dryrun", true, "是否只是演练（默认 true）")
}

func (c *MaintainDBCommand) PreRun(ctx context.Context) error {
	return nil
}

func (c *MaintainDBCommand) Run(ctx context.Context) error {
	dao := appdb.MetaDao
	logger := logutil.GetLogger(ctx)

	const pageSize = 500
	lastID := int64(0)
	totalInvalid := 0
	var deleteList []string

	for {
		page, err := dao.FetchPage(ctx, lastID, pageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}

		for _, item := range page {
			reasons := findInvalidReasons(item)
			if len(reasons) == 0 {
				continue
			}
			totalInvalid++
			logger.Warn("invalid meta entry",
				zap.String("hash", item.Hash),
				zap.Int64("id", item.ID),
				zap.Strings("reasons", reasons),
			)
			if !c.dryRun {
				deleteList = append(deleteList, item.Hash)
			}
		}

		lastID = page[len(page)-1].ID
	}

	if !c.dryRun && len(deleteList) > 0 {
		const chunkSize = 200
		for start := 0; start < len(deleteList); start += chunkSize {
			end := start + chunkSize
			if end > len(deleteList) {
				end = len(deleteList)
			}
			chunk := deleteList[start:end]
			if err := dao.DeleteByHashes(ctx, chunk); err != nil {
				return fmt.Errorf("delete invalid hashes: %w", err)
			}
		}
		logger.Info("invalid hashes deleted", zap.Int("count", len(deleteList)))
	}

	logger.Info("maintain-db completed",
		zap.Int("invalid_records", totalInvalid),
		zap.Bool("dry_run", c.dryRun),
	)
	return nil
}

func (c *MaintainDBCommand) PostRun(ctx context.Context) error { return nil }

func findInvalidReasons(item appdb.StoredMeta) []string {
	var reasons []string
	entry := item.Entry
	if strings.TrimSpace(entry.Name) == "" {
		reasons = append(reasons, "empty name")
	}
	if strings.TrimSpace(entry.Desc) == "" {
		reasons = append(reasons, "empty desc")
	}
	if entry.Size == 0 {
		reasons = append(reasons, "size=0")
	}
	if len(entry.Media) == 0 {
		reasons = append(reasons, "no media")
	} else if !hasRequiredImageEntries(entry.Media) {
		reasons = append(reasons, "missing image media")
	}
	return reasons
}

func hasRequiredImageEntries(media []model.MediaEntry) bool {
	for _, item := range media {
		switch strings.ToLower(item.Type) {
		case "boxart", "boxfront", "screenshot":
			return true
		}
	}
	return false
}

func init() {
	RegisterRunner("maintain-db", func() IRunner { return NewMaintainDBCommand() })
}
