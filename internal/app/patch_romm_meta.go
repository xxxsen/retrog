package app

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/model"
	"github.com/xxxsen/retrog/internal/romm"
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type PatchRommMetaCommand struct {
	host        string
	session     string
	csrfToken   string
	allowUpdate bool
	limit       int
	dryRun      bool
}

func NewPatchRommMetaCommand() *PatchRommMetaCommand {
	return &PatchRommMetaCommand{
		limit: 72,
	}
}

func (c *PatchRommMetaCommand) Name() string { return "patch-romm-meta" }

func (c *PatchRommMetaCommand) Desc() string {
	return "根据 meta.db 补齐 RomM 平台中的元数据"
}

func (c *PatchRommMetaCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.host, "host", "", "RomM 服务地址，例如 https://romm.example.com")
	f.StringVar(&c.session, "romm-session", "", "RomM romm_session Cookie")
	f.StringVar(&c.csrfToken, "csrftoken", "", "RomM csrf token (romm_csrftoken)")
	f.BoolVar(&c.allowUpdate, "allow-update", false, "允许覆盖已有封面/描述，默认仅填补空缺")
	f.IntVar(&c.limit, "limit", 72, "单次拉取的 ROM 数量（最大 72）")
	f.BoolVar(&c.dryRun, "dryrun", false, "仅演练流程并打印信息，不进行更新")
}

func (c *PatchRommMetaCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.host) == "" {
		return errors.New("patch-romm-meta requires --host")
	}
	if strings.TrimSpace(c.session) == "" {
		return errors.New("patch-romm-meta requires --romm-session")
	}
	if strings.TrimSpace(c.csrfToken) == "" {
		return errors.New("patch-romm-meta requires --csrftoken")
	}
	if c.limit <= 0 || c.limit > 72 {
		c.limit = 72
	}

	logutil.GetLogger(ctx).Info("starting patch-romm-meta",
		zap.String("host", c.host),
		zap.Bool("allow_update", c.allowUpdate),
		zap.Bool("dry_run", c.dryRun),
		zap.Int("limit", c.limit),
	)
	return nil
}

func (c *PatchRommMetaCommand) Run(ctx context.Context) error {
	client, err := romm.New(c.host, c.session, c.csrfToken)
	if err != nil {
		return err
	}
	romm.SetDefaultClient(client)

	metaDAO := appdb.MetaDao
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	platforms, err := client.GetPlatforms(ctx)
	if err != nil {
		return err
	}

	logger := logutil.GetLogger(ctx)
	var processed, updated, skipped, missingMeta, skippedExisting, missingCover int

	for _, platform := range platforms {
		if platform.RomCount == 0 {
			continue
		}

		logger.Info("processing platform",
			zap.Int("platform_id", platform.ID),
			zap.Int("rom_count", platform.RomCount),
		)

		total := platform.RomCount
		offset := 0
		for offset < total {
			resp, err := client.ListRoms(ctx, platform.ID, c.limit, offset)
			if err != nil {
				return err
			}
			if resp.Total > 0 {
				total = resp.Total
			}
			if len(resp.Items) == 0 {
				break
			}

			hashList := collectHashes(resp.Items)
			if len(hashList) == 0 {
				offset += len(resp.Items)
				continue
			}

			if c.dryRun {
				for _, romItem := range resp.Items {
					logger.Info("dryrun rom",
						zap.Int("platform_id", platform.ID),
						zap.Int("rom_id", romItem.ID),
						zap.Bool("has_summary", strings.TrimSpace(romItem.Summary) != ""),
						zap.Bool("has_cover", hasExistingArtwork(romItem)),
					)
				}
				offset += len(resp.Items)
				continue
			}

			metaMap, _, err := metaDAO.FetchByHashes(ctx, hashList)
			if err != nil {
				return err
			}

			for _, romItem := range resp.Items {
				processed++
				hash := extractMD5(romItem)
				entry, ok := metaMap[hash]
				if !ok {
					missingMeta++
					logger.Warn("meta not found for rom",
						zap.Int("rom_id", romItem.ID),
						zap.String("md5", hash),
					)
					continue
				}

				if !c.allowUpdate && hasExistingArtwork(romItem) {
					skippedExisting++
					continue
				}

				coverMedia := selectCover(entry.Media)
				if coverMedia == nil {
					missingCover++
					logger.Warn("cover missing in meta",
						zap.Int("rom_id", romItem.ID),
						zap.String("md5", hash),
					)
					continue
				}

				summary := strings.TrimSpace(entry.Desc)
				if summary == "" && !c.allowUpdate {
					skipped++
					continue
				}
				if summary == "" {
					summary = romItem.Summary
				}

				artwork, err := downloadArtwork(ctx, store, *coverMedia)
				if err != nil {
					logger.Error("download artwork failed",
						zap.Int("rom_id", romItem.ID),
						zap.String("md5", hash),
						zap.Error(err),
					)
					skipped++
					continue
				}
				if artwork == nil {
					missingCover++
					continue
				}

				updateReq := romm.UpdateRomRequest{
					Name:    defaultString(entry.Name, romItem.Name),
					FsName:  defaultString(romItem.FsName, romItem.Name),
					Summary: summary,
					Artwork: artwork,
				}

				if err := client.UpdateRom(ctx, romItem.ID, updateReq); err != nil {
					logger.Error("update rom failed",
						zap.Int("rom_id", romItem.ID),
						zap.String("md5", hash),
						zap.Error(err),
					)
					skipped++
					continue
				}
				updated++
			}

			offset += len(resp.Items)
		}
	}

	logger.Info("patch-romm-meta finished",
		zap.Int("processed", processed),
		zap.Int("updated", updated),
		zap.Int("skipped_existing", skippedExisting),
		zap.Int("skipped_missing_desc", skipped),
		zap.Int("missing_cover", missingCover),
		zap.Int("missing_meta", missingMeta),
	)
	return nil
}

func (c *PatchRommMetaCommand) PostRun(ctx context.Context) error { return nil }

func collectHashes(items []romm.Rom) []string {
	hashes := make([]string, 0, len(items))
	for _, item := range items {
		hash := extractMD5(item)
		if hash == "" {
			continue
		}
		hashes = append(hashes, hash)
	}
	return hashes
}

func extractMD5(item romm.Rom) string {
	hash := strings.ToLower(strings.TrimSpace(item.MD5Hash))
	if hash != "" {
		return hash
	}
	for _, file := range item.Files {
		value := strings.ToLower(strings.TrimSpace(file.MD5Hash))
		if value != "" {
			return value
		}
	}
	return ""
}

func hasExistingArtwork(item romm.Rom) bool {
	return strings.TrimSpace(item.PathCoverSmall) != "" ||
		strings.TrimSpace(item.PathCoverLarge) != ""
}

func selectCover(media []model.MediaEntry) *model.MediaEntry {
	if len(media) == 0 {
		return nil
	}
	bestTypes := []string{"boxart", "boxfront", "logo"}
	for _, t := range bestTypes {
		for _, m := range media {
			if strings.EqualFold(m.Type, t) {
				copy := m
				return &copy
			}
		}
	}
	return nil
}

func downloadArtwork(ctx context.Context, store storage.Client, media model.MediaEntry) (*romm.UpdateRomArtwork, error) {
	if store == nil {
		return nil, errors.New("storage client not initialised")
	}
	key := media.Hash + media.Ext
	tempDir, err := os.MkdirTemp("", "retrog-romm-artwork-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	target := filepath.Join(tempDir, key)
	if err := store.DownloadToFile(ctx, key, target); err != nil {
		return nil, fmt.Errorf("download artwork %s: %w", key, err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read artwork %s: %w", key, err)
	}
	info, err := os.Stat(target)
	if err == nil && info.Size() == 0 {
		return nil, fmt.Errorf("artwork %s is empty", key)
	}
	detectedType := http.DetectContentType(data)
	contentType := detectedType
	if strings.TrimSpace(contentType) == "" || contentType == "application/octet-stream" {
		contentType = mime.TypeByExtension(strings.ToLower(media.Ext))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	filename := key
	if extList, _ := mime.ExtensionsByType(contentType); len(extList) > 0 {
		chosen := extList[0]
		if chosen != "" {
			filename = media.Hash + chosen
		}
	}

	return &romm.UpdateRomArtwork{
		Filename:    filename,
		ContentType: contentType,
		Data:        data,
	}, nil
}

func defaultString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func init() {
	RegisterRunner("patch-romm-meta", func() IRunner { return NewPatchRommMetaCommand() })
}
