package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"go.uber.org/zap"
)

type WebCommand struct {
	dir         string
	root        string
	bind        string
	uploadDir   string
	server      *http.Server
	assets      *assetStore
	collections []*collectionPayload
	dataMu      sync.RWMutex
}

type collectionPayload struct {
	ID           string         `json:"id"`
	Index        int            `json:"index"`
	Name         string         `json:"name"`
	DirName      string         `json:"dir_name"`
	DisplayName  string         `json:"display_name"`
	MetadataPath string         `json:"metadata_path"`
	RelativePath string         `json:"relative_path"`
	Games        []*gamePayload `json:"games"`
}

type gamePayload struct {
	ID          string          `json:"id"`
	Index       int             `json:"index"`
	XIndexID    int             `json:"x_index_id"`
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

const gameIndexEntryKey = "x-index-id"
const stagedUploadPrefix = "__upload__/"

type updateGameRequest struct {
	MetadataPath string          `json:"metadata_path"`
	XIndexID     int             `json:"x_index_id"`
	Fields       []*fieldPayload `json:"fields"`
}

type gameUpdateResponse struct {
	Collection *collectionPayload `json:"collection"`
	Game       *gamePayload       `json:"game"`
	FilePath   string             `json:"file_path,omitempty"`
}

type uploadMediaResponse struct {
	FilePath string        `json:"file_path"`
	Asset    *assetPayload `json:"asset"`
}

type fallbackAssetField struct {
	Name string
	Path string
}

type assetStore struct {
	root  string
	extra []string
	mu    sync.RWMutex
	files map[string]string
}

func NewWebCommand() *WebCommand { return &WebCommand{bind: ":8080"} }

func (c *WebCommand) Name() string { return "web" }

func (c *WebCommand) Desc() string {
	return "使用 Web UI 管理 ROM 元信息"
}

func (c *WebCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.dir, "dir", "", "ROM 根目录")
	f.StringVar(&c.bind, "bind", ":8080", "HTTP 监听地址，例如 0.0.0.0:8080")
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
	uploadDir, err := os.MkdirTemp("", "retrog_upload")
	if err != nil {
		return err
	}
	c.uploadDir = uploadDir
	c.assets.AddAllowedRoot(uploadDir)
	return nil
}

func (c *WebCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	collections, err := loadCollections(ctx, c.root, c.assets)
	if err != nil {
		return err
	}
	c.setCollections(collections)
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
	mux.HandleFunc("/api/games/update", c.handleUpdateGame)
	mux.HandleFunc("/api/games/upload", c.handleUploadMedia)

	srv := &http.Server{
		Addr:    c.bind,
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
	if c.uploadDir != "" {
		_ = os.RemoveAll(c.uploadDir)
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
	_ = enc.Encode(c.collectionsSnapshot())
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
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	http.ServeFile(w, r, path)
}

func (c *WebCommand) handleUpdateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req updateGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	metadataPath, err := c.resolveMetadataPath(req.MetadataPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.XIndexID <= 0 {
		http.Error(w, "x_index_id must be positive", http.StatusBadRequest)
		return
	}
	if err := c.updateGameMetadata(metadataPath, req.XIndexID, req.Fields); err != nil {
		http.Error(w, fmt.Sprintf("update game failed: %v", err), http.StatusInternalServerError)
		return
	}
	if err := c.reloadCollections(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("reload collections failed: %v", err), http.StatusInternalServerError)
		return
	}
	coll, game := c.findGamePayload(filepath.ToSlash(metadataPath), req.XIndexID)
	if coll == nil || game == nil {
		http.Error(w, "updated game not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(&gameUpdateResponse{Collection: coll, Game: game})
}

func (c *WebCommand) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		http.Error(w, fmt.Sprintf("parse form failed: %v", err), http.StatusBadRequest)
		return
	}
	metadataPath, err := c.resolveMetadataPath(r.FormValue("metadata_path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = metadataPath
	xIndexID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("x_index_id")))
	if err != nil {
		http.Error(w, "invalid x_index_id", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header == nil || header.Filename == "" {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}
	if xIndexID <= 0 {
		http.Error(w, "invalid x_index_id", http.StatusBadRequest)
		return
	}
	token, stagedPath, err := c.stageMediaUpload(header.Filename, file)
	if err != nil {
		http.Error(w, fmt.Sprintf("stage media failed: %v", err), http.StatusInternalServerError)
		return
	}
	payload, err := c.buildStagedAssetPayload(stagedPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("build asset payload failed: %v", err), http.StatusInternalServerError)
		return
	}
	resp := &uploadMediaResponse{
		FilePath: token,
		Asset:    payload,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func (c *WebCommand) stageMediaUpload(filename string, source multipart.File) (string, string, error) {
	if c.uploadDir == "" {
		return "", "", errors.New("upload directory not initialized")
	}
	name := sanitizeFileComponent(filename)
	if name == "" {
		name = "upload.bin"
	}
	stagedName := fmt.Sprintf("%d__%s", time.Now().UnixNano(), name)
	dest := filepath.Join(c.uploadDir, stagedName)
	out, err := os.Create(dest)
	if err != nil {
		return "", "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, source); err != nil {
		return "", "", err
	}
	return stagedUploadPrefix + stagedName, dest, nil
}

func (c *WebCommand) buildStagedAssetPayload(path string) (*assetPayload, error) {
	if c.assets == nil {
		return nil, errors.New("asset store not initialized")
	}
	id, err := c.assets.Register(path)
	if err != nil {
		return nil, err
	}
	return &assetPayload{
		Name:     filepath.Base(path),
		Type:     detectAssetType(path),
		URL:      "/api/assets/" + id,
		FileName: filepath.Base(path),
	}, nil
}

func (c *WebCommand) collectionsSnapshot() []*collectionPayload {
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	return append([]*collectionPayload(nil), c.collections...)
}

func (c *WebCommand) setCollections(cols []*collectionPayload) {
	c.dataMu.Lock()
	defer c.dataMu.Unlock()
	c.collections = cols
}

func (c *WebCommand) reloadCollections(ctx context.Context) error {
	cols, err := loadCollections(ctx, c.root, c.assets)
	if err != nil {
		return err
	}
	c.setCollections(cols)
	return nil
}

func (c *WebCommand) findGamePayload(metadataPath string, xIndexID int) (*collectionPayload, *gamePayload) {
	metadataPath = filepath.ToSlash(metadataPath)
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	for _, coll := range c.collections {
		if coll == nil {
			continue
		}
		if filepath.ToSlash(coll.MetadataPath) != metadataPath {
			continue
		}
		for _, game := range coll.Games {
			if game != nil && game.XIndexID == xIndexID {
				return coll, game
			}
		}
	}
	return nil, nil
}

func (c *WebCommand) resolveMetadataPath(meta string) (string, error) {
	meta = strings.TrimSpace(meta)
	if meta == "" {
		return "", errors.New("metadata_path is required")
	}
	fsPath := filepath.Clean(filepath.FromSlash(meta))
	if !c.assets.contains(fsPath) {
		return "", fmt.Errorf("metadata path %s outside root %s", fsPath, c.root)
	}
	info, err := os.Stat(fsPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", fsPath)
	}
	return fsPath, nil
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
		changed, err := ensureGameIndexes(doc)
		if err != nil {
			logger.Error("ensure game indexes failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		if changed {
			if err := metadata.WriteMetadataFile(path, doc); err != nil {
				logger.Error("write metadata failed", zap.String("path", path), zap.Error(err))
				return nil
			}
			logger.Info("metadata updated with x-index-id", zap.String("path", path))
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
	for _, coll := range result {
		coll.ID = buildCollectionID(coll.MetadataPath, coll.Index)
		sort.Slice(coll.Games, func(i, j int) bool {
			return strings.Compare(coll.Games[i].DisplayName, coll.Games[j].DisplayName) < 0
		})
		for _, game := range coll.Games {
			game.ID = buildGameID(coll.ID, game.Index)
		}
	}
	return result, nil
}

func ensureGameIndexes(doc *metadata.Document) (bool, error) {
	if doc == nil {
		return false, nil
	}
	used := make(map[int]struct{})
	maxID := 0
	changed := false
	for _, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindGame {
			continue
		}
		entry := blk.Entry(gameIndexEntryKey)
		id, ok := parseXIndexEntry(entry)
		if ok {
			if id > maxID {
				maxID = id
			}
			if _, exists := used[id]; exists {
				ok = false
			} else {
				used[id] = struct{}{}
				normalized := strconv.Itoa(id)
				if len(entry.Values) == 0 || entry.Values[0] != normalized || !entry.Inline {
					entry.Values = []string{normalized}
					entry.Inline = true
					changed = true
				}
			}
		}
		if !ok {
			maxID++
			setBlockXIndexID(blk, maxID)
			used[maxID] = struct{}{}
			changed = true
		}
	}
	return changed, nil
}

func setBlockXIndexID(blk *metadata.Block, id int) {
	if blk == nil {
		return
	}
	value := strconv.Itoa(id)
	entry := blk.Entry(gameIndexEntryKey)
	if entry != nil {
		entry.Values = []string{value}
		entry.Inline = true
		return
	}
	newEntry := &metadata.Entry{
		Key:    gameIndexEntryKey,
		Values: []string{value},
		Inline: true,
	}
	blk.Entries = append(blk.Entries, newEntry)
}

func parseXIndexEntry(entry *metadata.Entry) (int, bool) {
	if entry == nil || len(entry.Values) == 0 {
		return 0, false
	}
	value := strings.TrimSpace(entry.Values[0])
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func blockXIndexID(blk *metadata.Block) int {
	if blk == nil {
		return 0
	}
	if entry := blk.Entry(gameIndexEntryKey); entry != nil {
		if id, ok := parseXIndexEntry(entry); ok {
			return id
		}
	}
	return 0
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
	collectionOrder := -1
	var current *collectionPayload
	var result []*collectionPayload

	for _, blk := range doc.Blocks {
		if blk == nil {
			continue
		}
		switch blk.Kind {
		case metadata.KindCollection:
			collectionOrder++
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
				Index:        collectionOrder,
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
			gameIndex := len(current.Games)
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
			assets, fallbackFields := collectGameAssets(metadataDir, typed, romBase, store, logger)
			fields = appendFallbackAssetFields(fields, fallbackFields)
			game := &gamePayload{
				Index:       gameIndex,
				XIndexID:    blockXIndexID(blk),
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

func appendFallbackAssetFields(fields []*fieldPayload, fallback map[string]fallbackAssetField) []*fieldPayload {
	if len(fallback) == 0 {
		return fields
	}
	existing := make(map[string]struct{})
	for _, field := range fields {
		if field == nil {
			continue
		}
		if strings.HasPrefix(field.Key, "assets.") {
			name := normalizeAssetKey(strings.TrimPrefix(field.Key, "assets."))
			if name != "" {
				existing[name] = struct{}{}
			}
		}
	}
	for norm, item := range fallback {
		if _, ok := existing[norm]; ok {
			continue
		}
		name := item.Name
		if name == "" {
			continue
		}
		fields = append(fields, &fieldPayload{
			Key:    "assets." + name,
			Values: []string{item.Path},
		})
	}
	return fields
}

func (c *WebCommand) updateGameMetadata(metadataPath string, xIndexID int, fields []*fieldPayload) error {
	doc, err := metadata.ParseMetadataFile(metadataPath)
	if err != nil {
		return err
	}
	block, err := findGameBlockByIndexID(doc, xIndexID)
	if err != nil {
		return err
	}
	fields, err = c.materializeStagedFields(metadataPath, block, fields)
	if err != nil {
		return err
	}
	order, updates, err := combineFieldValues(fields)
	if err != nil {
		return err
	}
	entries, err := rebuildBlockEntries(block.Entries, order, updates)
	if err != nil {
		return err
	}
	block.Entries = entries
	return metadata.WriteMetadataFile(metadataPath, doc)
}

func findGameBlockByIndexID(doc *metadata.Document, xIndexID int) (*metadata.Block, error) {
	if doc == nil {
		return nil, errors.New("metadata document is empty")
	}
	for _, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindGame {
			continue
		}
		if blockXIndexID(blk) == xIndexID {
			return blk, nil
		}
	}
	return nil, fmt.Errorf("game with x-index-id %d not found", xIndexID)
}

func combineFieldValues(fields []*fieldPayload) ([]string, map[string][]string, error) {
	order := make([]string, 0, len(fields))
	values := make(map[string][]string)
	hasGame := false
	for _, field := range fields {
		if field == nil {
			continue
		}
		rawKey := strings.TrimSpace(field.Key)
		if rawKey == "" {
			return nil, nil, fmt.Errorf("field key cannot be empty")
		}
		key := strings.ToLower(rawKey)
		normalized := normalizeFieldValues(field.Values)
		if len(normalized) == 0 {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = append(values[key], normalized...)
		if key == "game" {
			hasGame = true
		}
	}
	if !hasGame {
		return nil, nil, errors.New("game entry is required")
	}
	return order, values, nil
}

func rebuildBlockEntries(original []*metadata.Entry, order []string, updates map[string][]string) ([]*metadata.Entry, error) {
	entries := make([]*metadata.Entry, 0, len(original)+len(updates))
	used := make(map[string]bool)
	for _, entry := range original {
		if entry == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(entry.Key))
		if vals, ok := updates[key]; ok {
			entry.Values = append([]string{}, vals...)
			entry.Inline = len(entry.Values) == 1
			entries = append(entries, entry)
			used[key] = true
			continue
		}
		if key == "game" {
			return nil, errors.New("game entry is required")
		}
		// field removed
	}
	for _, key := range order {
		if used[key] {
			continue
		}
		vals := updates[key]
		if len(vals) == 0 {
			continue
		}
		entries = append(entries, &metadata.Entry{
			Key:    key,
			Values: append([]string{}, vals...),
			Inline: len(vals) == 1,
		})
	}
	gameIdx := -1
	for idx, entry := range entries {
		if entry != nil && entry.Key == "game" {
			gameIdx = idx
			break
		}
	}
	if gameIdx == -1 {
		return nil, errors.New("game entry is required")
	}
	if gameIdx != 0 {
		entries[0], entries[gameIdx] = entries[gameIdx], entries[0]
	}
	return entries, nil
}

func (c *WebCommand) materializeStagedFields(metadataPath string, block *metadata.Block, fields []*fieldPayload) ([]*fieldPayload, error) {
	if c == nil || c.uploadDir == "" {
		return fields, nil
	}
	for _, field := range fields {
		if field == nil {
			continue
		}
		for idx, value := range field.Values {
			if !strings.HasPrefix(value, stagedUploadPrefix) {
				continue
			}
			rel, err := c.moveStagedFileToMedia(metadataPath, block, value)
			if err != nil {
				return nil, err
			}
			field.Values[idx] = rel
		}
	}
	return fields, nil
}

func (c *WebCommand) moveStagedFileToMedia(metadataPath string, block *metadata.Block, token string) (string, error) {
	if c.uploadDir == "" {
		return "", errors.New("upload directory not initialized")
	}
	stagedName := strings.TrimPrefix(token, stagedUploadPrefix)
	if stagedName == "" {
		return "", fmt.Errorf("invalid staged token %q", token)
	}
	source := filepath.Join(c.uploadDir, filepath.FromSlash(stagedName))
	if _, err := os.Stat(source); err != nil {
		return "", err
	}
	return moveFileToMedia(metadataPath, block, source, stagedName)
}

func normalizeFieldValues(values []string) []string {
	var out []string
	for _, value := range values {
		trimmed := strings.TrimRight(value, "\r")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func moveFileToMedia(metadataPath string, block *metadata.Block, sourcePath, stagedName string) (string, error) {
	metadataDir := filepath.Dir(metadataPath)
	romBase := deriveRomBase(extractBlockFiles(block))
	if romBase == "" {
		romBase = sanitizeFileComponent(getBlockTitle(block))
	}
	mediaDir := filepath.Join(metadataDir, "media")
	if romBase != "" {
		mediaDir = filepath.Join(mediaDir, romBase)
	}
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return "", err
	}
	baseName := stagedName
	if idx := strings.Index(baseName, "__"); idx >= 0 && idx+2 < len(baseName) {
		baseName = baseName[idx+2:]
	}
	if baseName == "" {
		baseName = filepath.Base(sourcePath)
	}
	destPath := filepath.Join(mediaDir, baseName)
	if err := os.Rename(sourcePath, destPath); err != nil {
		if err := copyFileContents(sourcePath, destPath); err != nil {
			return "", err
		}
		if err := os.Remove(sourcePath); err != nil {
			return "", err
		}
	}
	rel, err := filepath.Rel(metadataDir, destPath)
	if err != nil {
		rel = destPath
	}
	return filepath.ToSlash(rel), nil
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func extractBlockFiles(blk *metadata.Block) []string {
	var files []string
	if blk == nil {
		return files
	}
	for _, entry := range blk.Entries {
		if entry == nil {
			continue
		}
		switch entry.Key {
		case "file", "files":
			files = append(files, entry.Values...)
		}
	}
	return files
}

func getBlockTitle(blk *metadata.Block) string {
	if blk == nil {
		return ""
	}
	if entry := blk.Entry("game"); entry != nil && len(entry.Values) > 0 {
		return entry.Values[0]
	}
	return ""
}

func sanitizeFileComponent(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "\n", "_")
	name = strings.ReplaceAll(name, "\t", "_")
	name = strings.Trim(name, ". ")
	if len(name) > 128 {
		name = name[:128]
	}
	return name
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

func collectGameAssets(metadataDir string, game metadata.Game, romBase string, store *assetStore, logger *zap.Logger) ([]*assetPayload, map[string]fallbackAssetField) {
	type assetCandidate struct {
		name string
		path string
	}
	resolved := make(map[string]assetCandidate)
	fallbackValues := make(map[string]fallbackAssetField)
	metadataKeys := make(map[string]struct{})
	for name, assetPath := range game.Assets {
		norm := normalizeAssetKey(name)
		if norm == "" {
			continue
		}
		resolved[norm] = assetCandidate{name: name, path: resolveAssetPath(metadataDir, assetPath)}
		metadataKeys[norm] = struct{}{}
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
				norm := normalizeAssetKey(cleanKey)
				if norm == "" {
					continue
				}
				if _, exists := resolved[norm]; exists {
					continue
				}
				absPath := filepath.Join(mediaDir, key)
				resolved[norm] = assetCandidate{name: cleanKey, path: absPath}
				rel, relErr := filepath.Rel(metadataDir, absPath)
				if relErr != nil {
					rel = absPath
				}
				fallbackValues[norm] = fallbackAssetField{
					Name: cleanKey,
					Path: filepath.ToSlash(rel),
				}
			}
		}
	}

	names := make([]string, 0, len(resolved))
	for _, candidate := range resolved {
		names = append(names, candidate.name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	var out []*assetPayload
	for _, name := range names {
		norm := normalizeAssetKey(name)
		candidate, ok := resolved[norm]
		if !ok {
			continue
		}
		if candidate.path == "" {
			continue
		}
		id, err := store.Register(candidate.path)
		if err != nil {
			logger.Warn("skip asset", zap.String("game", game.Title), zap.String("asset", candidate.name), zap.String("path", candidate.path), zap.Error(err))
			continue
		}
		out = append(out, &assetPayload{
			Name:     candidate.name,
			Type:     detectAssetType(candidate.path),
			URL:      "/api/assets/" + id,
			FileName: filepath.Base(candidate.path),
		})
	}
	// remove fallback entries that already existed in metadata
	for norm, entry := range fallbackValues {
		if _, ok := metadataKeys[norm]; ok {
			delete(fallbackValues, norm)
			continue
		}
		if entry.Path == "" {
			delete(fallbackValues, norm)
		}
	}
	return out, fallbackValues
}

func normalizeAssetKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
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
	if strings.HasPrefix(path, prefix) {
		return true
	}
	for _, extra := range s.extra {
		if path == extra {
			return true
		}
		prefix := extra + string(os.PathSeparator)
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (s *assetStore) AddAllowedRoot(path string) {
	if s == nil {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	s.extra = append(s.extra, filepath.Clean(abs))
}

func buildCollectionID(metadataPath string, idx int) string {
	base := fmt.Sprintf("%s#%d", metadataPath, idx)
	sum := sha1.Sum([]byte(base))
	return "collection-" + hex.EncodeToString(sum[:6])
}

func buildGameID(collectionID string, idx int) string {
	return fmt.Sprintf("%s-game-%d", collectionID, idx)
}

func init() {
	RegisterRunner("web", func() IRunner { return NewWebCommand() })
}
