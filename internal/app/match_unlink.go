package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type MatchUnlinkCommand struct {
	unlinkPath string
	outputPath string
}

type unlinkInput struct {
	Unlink []unlinkEntry `json:"unlink"`
}

type unlinkEntry struct {
	Location string           `json:"location"`
	Count    int              `json:"count"`
	Files    []unlinkMetaFile `json:"files"`
}

type unlinkMetaFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

type matchResult struct {
	Location   string           `json:"location"`
	MissCount  int              `json:"miss-count"`
	MatchCount int              `json:"match-count"`
	Files      []unlinkMetaFile `json:"files"`
}

type matchOutput struct {
	Match []matchResult `json:"match"`
}

func NewMatchUnlinkCommand() *MatchUnlinkCommand {
	return &MatchUnlinkCommand{}
}

func (c *MatchUnlinkCommand) Name() string { return "match-unlink" }

func (c *MatchUnlinkCommand) Desc() string {
	return "统计 scan-unlink 输出中可匹配到 meta.db 元数据的 ROM 列表"
}

func (c *MatchUnlinkCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.unlinkPath, "unlink-meta", "", "scan-unlink 输出的 JSON 文件")
	f.StringVar(&c.outputPath, "output", "", "统计结果输出路径")
}

func (c *MatchUnlinkCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.unlinkPath) == "" {
		return errors.New("match-unlink requires --unlink-meta")
	}
	if strings.TrimSpace(c.outputPath) == "" {
		return errors.New("match-unlink requires --output")
	}
	logutil.GetLogger(ctx).Info("starting match-unlink",
		zap.String("unlink_meta", c.unlinkPath),
		zap.String("output", c.outputPath),
	)
	return nil
}

func (c *MatchUnlinkCommand) Run(ctx context.Context) error {
	data, err := os.ReadFile(c.unlinkPath)
	if err != nil {
		return fmt.Errorf("read unlink meta %s: %w", c.unlinkPath, err)
	}
	var input unlinkInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("decode unlink meta %s: %w", c.unlinkPath, err)
	}

	hashSet := make(map[string]struct{})
	for _, entry := range input.Unlink {
		for _, file := range entry.Files {
			hash := normalizeHash(file.Hash)
			if hash == "" {
				continue
			}
			hashSet[hash] = struct{}{}
		}
	}

	matched, err := fetchMatchedHashes(ctx, appdb.MetaDao, hashSet)
	if err != nil {
		return err
	}

	results := make([]matchResult, 0)
	for _, entry := range input.Unlink {
		matchedFiles := make([]unlinkMetaFile, 0)
		for _, file := range entry.Files {
			hash := normalizeHash(file.Hash)
			if hash == "" {
				continue
			}
			if _, ok := matched[hash]; ok {
				matchedFiles = append(matchedFiles, file)
			}
		}
		if len(matchedFiles) == 0 {
			continue
		}
		results = append(results, matchResult{
			Location:   filepath.ToSlash(entry.Location),
			MissCount:  entry.Count,
			MatchCount: len(matchedFiles),
			Files:      matchedFiles,
		})
	}

	output := matchOutput{Match: results}
	outData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal match output: %w", err)
	}
	if err := os.WriteFile(c.outputPath, outData, 0o644); err != nil {
		return fmt.Errorf("write match output %s: %w", c.outputPath, err)
	}

	logutil.GetLogger(ctx).Info("match-unlink completed",
		zap.Int("locations", len(results)),
		zap.String("output", c.outputPath),
	)
	return nil
}

func (c *MatchUnlinkCommand) PostRun(ctx context.Context) error { return nil }

func fetchMatchedHashes(ctx context.Context, dao *appdb.MetaDAO, hashSet map[string]struct{}) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(hashSet))
	if len(hashSet) == 0 {
		return result, nil
	}

	hashes := make([]string, 0, len(hashSet))
	for hash := range hashSet {
		hashes = append(hashes, hash)
	}

	const chunkSize = 200
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		entries, _, err := dao.FetchByHashes(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for hash := range entries {
			result[normalizeHash(hash)] = struct{}{}
		}
	}

	return result, nil
}

func normalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func init() {
	RegisterRunner("match-unlink", func() IRunner { return NewMatchUnlinkCommand() })
}
