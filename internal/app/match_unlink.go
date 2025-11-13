package app

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/model"
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type MatchUnlinkCommand struct {
	unlinkPath string
	outputPath string
	fix        bool
	replace    bool
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

type matchedMeta struct {
	Entry model.Entry
	ID    int64
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
	f.BoolVar(&c.fix, "fix", false, "根据匹配结果补充 gamelist.xml")
	f.BoolVar(&c.replace, "replace", false, "与 --fix 搭配使用，允许直接覆盖 gamelist.xml；默认输出 gamelist.xml.fix")
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
		zap.Bool("fix", c.fix),
		zap.Bool("replace", c.replace),
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

	matchedEntries, err := fetchMatchedEntries(ctx, appdb.MetaDao, hashSet)
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
			if _, ok := matchedEntries[hash]; ok {
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

	if c.fix && len(results) > 0 {
		if err := c.fixGamelists(ctx, results, matchedEntries); err != nil {
			return err
		}
	}

	return nil
}

func (c *MatchUnlinkCommand) PostRun(ctx context.Context) error { return nil }

func fetchMatchedEntries(ctx context.Context, dao *appdb.MetaDAO, hashSet map[string]struct{}) (map[string]matchedMeta, error) {
	result := make(map[string]matchedMeta, len(hashSet))
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
		entries, _, err := dao.FetchStoredByHashes(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for hash, stored := range entries {
			result[normalizeHash(hash)] = matchedMeta{
				Entry: stored.Entry,
				ID:    stored.ID,
			}
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

const (
	imageDir     = "images"
	videoDir     = "videos"
	retrogSubdir = "retrog"
)

func (c *MatchUnlinkCommand) fixGamelists(ctx context.Context, results []matchResult, entries map[string]matchedMeta) error {
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	logger := logutil.GetLogger(ctx)
	for _, result := range results {
		dir := filepath.Clean(result.Location)
		gamelistPath := filepath.Join(dir, gamelistFileName)
		if _, err := os.Stat(gamelistPath); err != nil {
			logger.Warn("gamelist missing, skip fix",
				zap.String("path", filepath.ToSlash(gamelistPath)),
				zap.Error(err),
			)
			continue
		}

		additions := make([]string, 0, len(result.Files))
		for _, file := range result.Files {
			meta, ok := entries[normalizeHash(file.Hash)]
			if !ok {
				continue
			}
			xmlSnippet, err := c.buildGameXML(ctx, store, dir, file, meta)
			if err != nil {
				logger.Error("build game xml failed",
					zap.String("location", dir),
					zap.String("rom", file.Name),
					zap.Error(err),
				)
				continue
			}
			additions = append(additions, xmlSnippet)
		}
		if len(additions) == 0 {
			continue
		}

		destPath := gamelistPath
		if !c.replace {
			destPath = gamelistPath + ".fix"
		}
		if err := appendGamesToGamelist(gamelistPath, destPath, additions); err != nil {
			return err
		}
		logger.Info("gamelist updated",
			zap.String("path", filepath.ToSlash(destPath)),
			zap.Int("added", len(additions)),
		)
	}
	return nil
}

func (c *MatchUnlinkCommand) buildGameXML(ctx context.Context, store storage.Client, dir string, file unlinkMetaFile, meta matchedMeta) (string, error) {
	entry := meta.Entry
	name := entry.Name
	if strings.TrimSpace(name) == "" {
		name = file.Name
	}

	pathValue := "./" + file.Name
	desc := strings.TrimSpace(entry.Desc)

	imagePath, err := c.downloadMedia(ctx, store, dir, imageDir, entry.Media, []string{"boxart", "boxfront", "logo", "screenshot"})
	if err != nil {
		return "", err
	}
	videoPath, err := c.downloadMedia(ctx, store, dir, videoDir, entry.Media, []string{"video"})
	if err != nil {
		return "", err
	}

	var release string
	if entry.ReleaseAt > 0 {
		release = time.Unix(entry.ReleaseAt, 0).UTC().Format("20060102T150405")
	}

	genres := strings.Join(entry.Genres, ",")

	var builder strings.Builder
	builder.WriteString("  <game")
	idValue := fmt.Sprintf("%d", meta.ID)
	if strings.TrimSpace(idValue) == "" {
		idValue = "0"
	}
	builder.WriteString(` id="`)
	builder.WriteString(escapeXML(idValue))
	builder.WriteString(`" source="retrog">`)
	builder.WriteByte('\n')
	writeXMLElement(&builder, "path", pathValue)
	writeXMLElement(&builder, "name", name)
	if desc != "" {
		writeXMLElement(&builder, "desc", desc)
	}
	if imagePath != "" {
		writeXMLElement(&builder, "image", imagePath)
		writeXMLElement(&builder, "marquee", imagePath)
	}
	if videoPath != "" {
		writeXMLElement(&builder, "video", videoPath)
	}
	if release != "" {
		writeXMLElement(&builder, "releasedate", release)
	}
	if entry.Developer != "" {
		writeXMLElement(&builder, "developer", entry.Developer)
	}
	if entry.Publisher != "" {
		writeXMLElement(&builder, "publisher", entry.Publisher)
	}
	if genres != "" {
		writeXMLElement(&builder, "genre", genres)
	}
	writeXMLElement(&builder, "md5", normalizeHash(file.Hash))
	builder.WriteString("  </game>\n")

	return builder.String(), nil
}

func (c *MatchUnlinkCommand) downloadMedia(ctx context.Context, store storage.Client, dir string, category string, mediaList []model.MediaEntry, preferred []string) (string, error) {
	target := pickMedia(mediaList, preferred)
	if target == nil {
		return "", nil
	}

	subdir := filepath.Join(category, retrogSubdir)
	destDir := filepath.Join(dir, subdir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create media dir %s: %w", destDir, err)
	}

	filename := target.Hash + target.Ext
	destPath := filepath.Join(destDir, filename)
	if _, err := os.Stat(destPath); errors.Is(err, os.ErrNotExist) {
		if err := store.DownloadToFile(ctx, target.Hash+target.Ext, destPath); err != nil {
			return "", fmt.Errorf("download media %s: %w", target.Hash+target.Ext, err)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat media %s: %w", destPath, err)
	}

	rel := "./" + filepath.ToSlash(filepath.Join(subdir, filename))
	return rel, nil
}

func pickMedia(media []model.MediaEntry, preferred []string) *model.MediaEntry {
	for _, typ := range preferred {
		for _, item := range media {
			if strings.EqualFold(item.Type, typ) {
				copy := item
				return &copy
			}
		}
	}
	return nil
}

func writeXMLElement(builder *strings.Builder, tag, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	builder.WriteString("    <")
	builder.WriteString(tag)
	builder.WriteString(">")
	builder.WriteString(escapeXML(value))
	builder.WriteString("</")
	builder.WriteString(tag)
	builder.WriteString(">\n")
}

func escapeXML(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}

func appendGamesToGamelist(srcPath, destPath string, games []string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read gamelist %s: %w", srcPath, err)
	}
	closingTag := []byte("</gameList>")
	idx := bytes.LastIndex(data, closingTag)
	if idx == -1 {
		return fmt.Errorf("invalid gamelist %s: missing </gameList>", srcPath)
	}

	var addition strings.Builder
	addition.WriteByte('\n')
	for _, game := range games {
		addition.WriteString(game)
	}

	var output bytes.Buffer
	output.Write(data[:idx])
	output.WriteString(addition.String())
	output.Write(data[idx:])

	if err := os.WriteFile(destPath, output.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write gamelist %s: %w", destPath, err)
	}
	return nil
}
