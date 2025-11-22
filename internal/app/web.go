package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"go.uber.org/zap"
)

type WebCommand struct {
	dir         string
	root        string
	server      *http.Server
	assets      *assetStore
	collections []*collectionPayload
}

type collectionPayload struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	DirName      string         `json:"dir_name"`
	DisplayName  string         `json:"display_name"`
	MetadataPath string         `json:"metadata_path"`
	RelativePath string         `json:"relative_path"`
	Games        []*gamePayload `json:"games"`
}

type gamePayload struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	RomPath     string          `json:"rom_path"`
	DisplayName string          `json:"display_name"`
	Fields      []*fieldPayload `json:"fields"`
	Assets      []*assetPayload `json:"assets"`
}

type fieldPayload struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

type assetPayload struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	FileName string `json:"file_name"`
}

type assetStore struct {
	root  string
	mu    sync.RWMutex
	files map[string]string
}

func NewWebCommand() *WebCommand { return &WebCommand{} }

func (c *WebCommand) Name() string { return "web" }

func (c *WebCommand) Desc() string {
	return "使用 Web UI 管理 ROM 元信息"
}

func (c *WebCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.dir, "dir", "", "ROM 根目录")
}

func (c *WebCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.dir) == "" {
		return errors.New("web requires --dir")
	}
	absDir, err := filepath.Abs(c.dir)
	if err != nil {
		return err
	}
	c.root = filepath.Clean(absDir)
	info, err := os.Stat(c.root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", c.root)
	}
	store, err := newAssetStore(c.root)
	if err != nil {
		return err
	}
	c.assets = store
	return nil
}

func (c *WebCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	collections, err := loadCollections(ctx, c.root, c.assets)
	if err != nil {
		return err
	}
	c.collections = collections
	logger.Info("metadata loaded",
		zap.Int("collection_count", len(collections)))

	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handleIndex)
	staticFS, err := fs.Sub(webContent, "webui/static")
	if err != nil {
		return err
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/api/collections", c.handleCollections)
	mux.HandleFunc("/api/assets/", c.handleAsset)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	c.server = srv

	logger.Info("web ui ready",
		zap.String("addr", srv.Addr),
		zap.String("root", c.root))

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (c *WebCommand) PostRun(ctx context.Context) error {
	if c.server != nil {
		_ = c.server.Close()
	}
	return nil
}

func (c *WebCommand) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := webContent.ReadFile("webui/index.html")
	if err != nil {
		http.Error(w, "failed to load ui", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (c *WebCommand) handleCollections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(c.collections)
}

func (c *WebCommand) handleAsset(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/assets/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	path, ok := c.assets.Lookup(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func loadCollections(ctx context.Context, root string, store *assetStore) ([]*collectionPayload, error) {
	logger := logutil.GetLogger(ctx)
	var result []*collectionPayload
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(d.Name(), constant.DefaultMetadataFile) {
			return nil
		}
		doc, err := metadata.ParseMetadataFile(path)
		if err != nil {
			logger.Error("parse metadata failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		colls, err := buildCollections(doc, path, root, store, logger)
		if err != nil {
			logger.Error("build collection view failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		result = append(result, colls...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].DisplayName, result[j].DisplayName) < 0
	})
	for idx, coll := range result {
		coll.ID = fmt.Sprintf("collection-%d", idx+1)
		sort.Slice(coll.Games, func(i, j int) bool {
			return strings.Compare(coll.Games[i].DisplayName, coll.Games[j].DisplayName) < 0
		})
		for gidx, game := range coll.Games {
			game.ID = fmt.Sprintf("%s-game-%d", coll.ID, gidx+1)
		}
	}
	return result, nil
}

func buildCollections(doc *metadata.Document, metadataPath, root string, store *assetStore, logger *zap.Logger) ([]*collectionPayload, error) {
	metadataDir := filepath.Dir(metadataPath)
	dirName := filepath.Base(metadataDir)
	relDir, err := filepath.Rel(root, metadataDir)
	if err != nil {
		relDir = metadataDir
	}
	relDir = filepath.ToSlash(relDir)
	metadataPath = filepath.ToSlash(metadataPath)

	typedCollections, _ := doc.Collections()
	typedGames, _ := doc.Games()
	collIdx := 0
	gameIdx := 0
	var current *collectionPayload
	var result []*collectionPayload

	for _, blk := range doc.Blocks {
		if blk == nil {
			continue
		}
		switch blk.Kind {
		case metadata.KindCollection:
			var typed metadata.Collection
			if collIdx < len(typedCollections) {
				typed = typedCollections[collIdx]
			}
			collIdx++
			name := strings.TrimSpace(typed.Name)
			if name == "" {
				if entry := blk.Entry("collection"); entry != nil && len(entry.Values) > 0 {
					name = strings.Join(entry.Values, "\n")
				}
			}
			if name == "" {
				name = dirName
			}
			current = &collectionPayload{
				Name:         name,
				DirName:      dirName,
				DisplayName:  fmt.Sprintf("%s(%s)", name, dirName),
				MetadataPath: metadataPath,
				RelativePath: relDir,
			}
			result = append(result, current)
		case metadata.KindGame:
			if current == nil {
				continue
			}
			var typed metadata.Game
			if gameIdx < len(typedGames) {
				typed = typedGames[gameIdx]
			}
			gameIdx++
			title := strings.TrimSpace(typed.Title)
			if title == "" {
				if entry := blk.Entry("game"); entry != nil && len(entry.Values) > 0 {
					title = strings.Join(entry.Values, "\n")
				}
			}
			fields := convertBlockFields(blk)
			romPath := resolveRomPath(metadataDir, typed.Files)
			if romPath == "" && len(typed.Files) > 0 {
				romPath = filepath.ToSlash(strings.TrimSpace(typed.Files[0]))
			}
			display := title
			if romPath != "" {
				display = fmt.Sprintf("%s (%s)", title, romPath)
			}
			romBase := deriveRomBase(typed.Files)
			assets := collectGameAssets(metadataDir, typed, romBase, store, logger)
			game := &gamePayload{
				Title:       title,
				RomPath:     romPath,
				DisplayName: display,
				Fields:      fields,
				Assets:      assets,
			}
			current.Games = append(current.Games, game)
		}
	}
	return result, nil
}

func convertBlockFields(blk *metadata.Block) []*fieldPayload {
	var out []*fieldPayload
	if blk == nil {
		return out
	}
	for _, entry := range blk.Entries {
		if entry == nil {
			continue
		}
		cp := make([]string, len(entry.Values))
		copy(cp, entry.Values)
		out = append(out, &fieldPayload{Key: entry.Key, Values: cp})
	}
	return out
}

func resolveRomPath(baseDir string, files []string) string {
	for _, file := range files {
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			continue
		}
		if filepath.IsAbs(trimmed) {
			return filepath.ToSlash(filepath.Clean(trimmed))
		}
		joined := filepath.Join(baseDir, trimmed)
		return filepath.ToSlash(filepath.Clean(joined))
	}
	return ""
}

func deriveRomBase(files []string) string {
	for _, file := range files {
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			continue
		}
		normalized := strings.ReplaceAll(trimmed, "\\", "/")
		base := path.Base(normalized)
		base = strings.TrimSuffix(base, path.Ext(base))
		base = strings.TrimSpace(base)
		if base != "" && base != "." {
			return base
		}
	}
	return ""
}

func collectGameAssets(metadataDir string, game metadata.Game, romBase string, store *assetStore, logger *zap.Logger) []*assetPayload {
	resolved := make(map[string]string)
	for name, assetPath := range game.Assets {
		resolved[name] = resolveAssetPath(metadataDir, assetPath)
	}
	if romBase != "" {
		mediaDir := filepath.Join(metadataDir, "media", romBase)
		entries, err := os.ReadDir(mediaDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				key := entry.Name()
				cleanKey := strings.TrimSuffix(key, filepath.Ext(key))
				if cleanKey == "" {
					cleanKey = key
				}
				if _, exists := resolved[cleanKey]; exists {
					continue
				}
				resolved[cleanKey] = filepath.Join(mediaDir, key)
			}
		}
	}

	names := make([]string, 0, len(resolved))
	for name := range resolved {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []*assetPayload
	for _, name := range names {
		path := resolved[name]
		if path == "" {
			continue
		}
		id, err := store.Register(path)
		if err != nil {
			logger.Warn("skip asset", zap.String("game", game.Title), zap.String("asset", name), zap.String("path", path), zap.Error(err))
			continue
		}
		out = append(out, &assetPayload{
			Name:     name,
			Type:     detectAssetType(path),
			URL:      "/api/assets/" + id,
			FileName: filepath.Base(path),
		})
	}
	return out
}

func resolveAssetPath(baseDir, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(baseDir, trimmed))
}

func detectAssetType(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return "image"
	case ".mp4", ".webm", ".mov", ".avi", ".mkv":
		return "video"
	default:
		return "other"
	}
}

func newAssetStore(root string) (*assetStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &assetStore{root: filepath.Clean(abs), files: make(map[string]string)}, nil
}

func (s *assetStore) Register(path string) (string, error) {
	if s == nil {
		return "", errors.New("asset store not initialized")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if !s.contains(abs) {
		return "", fmt.Errorf("asset %s outside root %s", abs, s.root)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("asset %s is a directory", abs)
	}
	sum := sha1.Sum([]byte(abs))
	id := hex.EncodeToString(sum[:])
	s.mu.Lock()
	s.files[id] = abs
	s.mu.Unlock()
	return id, nil
}

func (s *assetStore) Lookup(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, ok := s.files[id]
	return path, ok
}

func (s *assetStore) contains(path string) bool {
	if path == s.root {
		return true
	}
	prefix := s.root + string(os.PathSeparator)
	return strings.HasPrefix(path, prefix)
}

func init() {
	RegisterRunner("web", func() IRunner { return NewWebCommand() })
}
