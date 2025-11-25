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
	"github.com/xxxsen/retrog/internal/webui"
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
	ID           string          `json:"id"`
	Index        int             `json:"index"`
	XIndexID     int             `json:"x_index_id"`
	Available    int             `json:"available_games"`
	Total        int             `json:"total_games"`
	Name         string          `json:"name"`
	DirName      string          `json:"dir_name"`
	DisplayName  string          `json:"display_name"`
	MetadataPath string          `json:"metadata_path"`
	RelativePath string          `json:"relative_path"`
	SortKey      string          `json:"sort_key"`
	Extensions   []string        `json:"extensions,omitempty"`
	Fields       []*fieldPayload `json:"fields"`
	Games        []*gamePayload  `json:"games"`
}

type gamePayload struct {
	ID          string          `json:"id"`
	Index       int             `json:"index"`
	XIndexID    int             `json:"x_index_id"`
	Title       string          `json:"title"`
	RomPath     string          `json:"rom_path"`
	RelRomPath  string          `json:"rel_rom_path"`
	DisplayName string          `json:"display_name"`
	SortKey     string          `json:"sort_key"`
	RomMissing  bool            `json:"rom_missing"`
	HasBoxArt   bool            `json:"has_boxart"`
	HasVideo    bool            `json:"has_video"`
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

const xIndexEntryKey = "x-index-id"
const stagedUploadPrefix = "__upload__/"

type updateGameRequest struct {
	MetadataPath string          `json:"metadata_path"`
	XIndexID     int             `json:"x_index_id"`
	Fields       []*fieldPayload `json:"fields"`
	Removed      []*fieldPayload `json:"removed_fields"`
}

type createGameRequest struct {
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

type updateCollectionRequest struct {
	MetadataPath string          `json:"metadata_path"`
	XIndexID     int             `json:"x_index_id"`
	Fields       []*fieldPayload `json:"fields"`
}

type collectionUpdateResponse struct {
	Collection *collectionPayload `json:"collection"`
}

type fallbackAssetField struct {
	Name string
	Path string
}

type deleteGameRequest struct {
	MetadataPath string `json:"metadata_path"`
	XIndexID     int    `json:"x_index_id"`
	RemoveFiles  bool   `json:"remove_files"`
}

type deleteGameResponse struct {
	Collection *collectionPayload `json:"collection"`
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
	staticFS, err := fs.Sub(webui.Content, "static")
	if err != nil {
		return err
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/api/collections/update", c.handleUpdateCollection)
	mux.HandleFunc("/api/collections", c.handleCollections)
	mux.HandleFunc("/api/assets/", c.handleAsset)
	mux.HandleFunc("/api/games/update", c.handleUpdateGame)
	mux.HandleFunc("/api/games/upload", c.handleUploadMedia)
	mux.HandleFunc("/api/games/create", c.handleCreateGame)
	mux.HandleFunc("/api/games/delete", c.handleDeleteGame)

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
	data, err := webui.Content.ReadFile("index.html")
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

func (c *WebCommand) handleUpdateCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req updateCollectionRequest
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
	if err := c.updateCollectionMetadata(metadataPath, req.XIndexID, req.Fields); err != nil {
		http.Error(w, fmt.Sprintf("update collection failed: %v", err), http.StatusInternalServerError)
		return
	}
	if err := c.reloadCollections(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("reload collections failed: %v", err), http.StatusInternalServerError)
		return
	}
	coll := c.findCollectionByIndex(filepath.ToSlash(metadataPath), req.XIndexID)
	if coll == nil {
		http.Error(w, "collection not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(&collectionUpdateResponse{Collection: coll})
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
	if err := c.updateGameMetadata(metadataPath, req.XIndexID, req.Fields, req.Removed); err != nil {
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

func (c *WebCommand) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req createGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	metadataPath, err := c.resolveMetadataPath(req.MetadataPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	xIndexID, err := c.createGame(metadataPath, req.XIndexID, req.Fields)
	if err != nil {
		http.Error(w, fmt.Sprintf("create game failed: %v", err), http.StatusInternalServerError)
		return
	}
	if err := c.reloadCollections(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("reload collections failed: %v", err), http.StatusInternalServerError)
		return
	}
	coll, game := c.findGamePayload(filepath.ToSlash(metadataPath), xIndexID)
	if coll == nil || game == nil {
		http.Error(w, "created game not found", http.StatusNotFound)
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
	fieldKey := strings.ToLower(strings.TrimSpace(r.FormValue("field")))
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
	if isRomFieldKey(fieldKey) {
		doc, err := metadata.ParseMetadataFile(metadataPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("load metadata failed: %v", err), http.StatusInternalServerError)
			return
		}
		var currentBlock *metadata.Block
		if blk, _, err := findGameBlockByIndexID(doc, xIndexID); err == nil {
			currentBlock = blk
		}
		if err := ensureUniqueRomUpload(doc, currentBlock, header.Filename); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
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

func (c *WebCommand) handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req deleteGameRequest
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
	if err := c.deleteGame(metadataPath, req.XIndexID, req.RemoveFiles); err != nil {
		http.Error(w, fmt.Sprintf("delete game failed: %v", err), http.StatusInternalServerError)
		return
	}
	if err := c.reloadCollections(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("reload collections failed: %v", err), http.StatusInternalServerError)
		return
	}
	coll := c.findCollectionByPath(filepath.ToSlash(metadataPath))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(&deleteGameResponse{Collection: coll})
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

func (c *WebCommand) findCollectionByPath(metadataPath string) *collectionPayload {
	metadataPath = filepath.ToSlash(metadataPath)
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	for _, coll := range c.collections {
		if coll != nil && filepath.ToSlash(coll.MetadataPath) == metadataPath {
			return coll
		}
	}
	return nil
}

func (c *WebCommand) findCollectionByIndex(metadataPath string, xIndexID int) *collectionPayload {
	metadataPath = filepath.ToSlash(metadataPath)
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	for _, coll := range c.collections {
		if coll == nil {
			continue
		}
		if filepath.ToSlash(coll.MetadataPath) == metadataPath && coll.XIndexID == xIndexID {
			return coll
		}
	}
	return nil
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
		collChanged, err := ensureCollectionIndexes(doc)
		if err != nil {
			logger.Error("ensure collection indexes failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		gameChanged, err := ensureGameIndexes(doc)
		if err != nil {
			logger.Error("ensure game indexes failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		if collChanged || gameChanged {
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
		return compareCollectionSortKey(result[i], result[j])
	})
	for _, coll := range result {
		coll.ID = buildCollectionID(coll.MetadataPath, coll.Index)
		sort.Slice(coll.Games, func(i, j int) bool {
			return compareGameSortKey(coll.Games[i], coll.Games[j])
		})
		for _, game := range coll.Games {
			game.ID = buildGameID(coll.ID, game.Index)
		}
	}
	return result, nil
}

func ensureCollectionIndexes(doc *metadata.Document) (bool, error) {
	if doc == nil {
		return false, nil
	}
	used := make(map[int]struct{})
	maxID := 0
	changed := false
	for _, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindCollection {
			continue
		}
		entry := blk.Entry(xIndexEntryKey)
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
		entry := blk.Entry(xIndexEntryKey)
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
	entry := blk.Entry(xIndexEntryKey)
	if entry != nil {
		entry.Values = []string{value}
		entry.Inline = true
		return
	}
	newEntry := &metadata.Entry{
		Key:    xIndexEntryKey,
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
	if entry := blk.Entry(xIndexEntryKey); entry != nil {
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
				XIndexID:     blockXIndexID(blk),
				Name:         name,
				DirName:      dirName,
				DisplayName:  fmt.Sprintf("%s(%s)", name, dirName),
				MetadataPath: metadataPath,
				RelativePath: relDir,
				SortKey:      strings.TrimSpace(typed.SortBy),
				Extensions:   parseCollectionExtensions(blk),
				Fields:       convertBlockFields(blk),
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
			existingRomPath := findExistingRomPath(metadataDir, typed.Files)
			romMissing := existingRomPath == ""
			if existingRomPath != "" {
				romPath = existingRomPath
			}
			relRomPath := romPath
			if rel, err := filepath.Rel(root, filepath.FromSlash(romPath)); err == nil {
				relRomPath = filepath.ToSlash(rel)
			}
			display := title
			if romPath != "" {
				display = fmt.Sprintf("%s (%s)", title, romPath)
			}
			romBase := deriveRomBase(typed.Files)
			assets, fallbackFields := collectGameAssets(metadataDir, typed, romBase, romMissing, store, logger)
			fields = appendFallbackAssetFields(fields, fallbackFields)
			hasBoxArt := fieldExists(fields, "assets.boxfront")
			if !hasBoxArt {
				hasBoxArt = fieldExists(fields, "assets.boxart")
			}
			hasVideo := fieldExists(fields, "assets.video")
			game := &gamePayload{
				Index:       gameIndex,
				XIndexID:    blockXIndexID(blk),
				Title:       title,
				RomPath:     romPath,
				RelRomPath:  relRomPath,
				DisplayName: display,
				SortKey:     strings.TrimSpace(typed.SortBy),
				RomMissing:  romMissing,
				HasBoxArt:   hasBoxArt,
				HasVideo:    hasVideo,
				Fields:      fields,
				Assets:      assets,
			}
			current.Games = append(current.Games, game)
			current.Total++
			if !romMissing {
				current.Available++
			}
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
		for idx, value := range entry.Values {
			cp[idx] = normalizeFieldValueForDisplay(entry.Key, value)
		}
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

func fieldExists(fields []*fieldPayload, key string) bool {
	key = strings.ToLower(key)
	for _, field := range fields {
		if field == nil {
			continue
		}
		if strings.ToLower(field.Key) == key {
			return true
		}
	}
	return false
}

func isRomFieldKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "file", "files":
		return true
	default:
		return false
	}
}

func ensureUniqueRomUpload(doc *metadata.Document, currentBlock *metadata.Block, filename string) error {
	normalized := normalizedRomFileName(filename)
	if normalized == "" {
		return nil
	}
	if romFileExistsInDocument(doc, currentBlock, normalized) {
		name := sanitizeFileComponent(filename)
		if name == "" {
			name = filename
		}
		if strings.TrimSpace(name) == "" {
			name = "ROM"
		}
		return fmt.Errorf("ROM 已存在: %s", name)
	}
	return nil
}

func romFileExistsInDocument(doc *metadata.Document, skipBlock *metadata.Block, normalizedName string) bool {
	if doc == nil || normalizedName == "" {
		return false
	}
	for _, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindGame {
			continue
		}
		if skipBlock != nil && blk == skipBlock {
			continue
		}
		for _, value := range extractBlockFiles(blk) {
			if normalizedRomFileName(value) == normalizedName {
				return true
			}
		}
	}
	return false
}

func normalizedRomFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\", "/")
	base := path.Base(value)
	base = sanitizeFileComponent(base)
	if base == "" || base == "." {
		return ""
	}
	return strings.ToLower(base)
}

func allowedExtensionsForGame(doc *metadata.Document, gameBlock *metadata.Block) []string {
	if doc == nil || gameBlock == nil {
		return nil
	}
	var current *metadata.Block
	for _, blk := range doc.Blocks {
		if blk == nil {
			continue
		}
		if blk.Kind == metadata.KindCollection {
			current = blk
		}
		if blk == gameBlock {
			if current == nil {
				return nil
			}
			return parseCollectionExtensions(current)
		}
	}
	return nil
}

func parseCollectionExtensions(blk *metadata.Block) []string {
	if blk == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	for _, entry := range blk.Entries {
		if entry == nil {
			continue
		}
		switch entry.Key {
		case "extensions", "extension":
			for _, value := range entry.Values {
				for _, part := range strings.Split(value, ",") {
					normalized := normalizeExtension(part)
					if normalized == "" {
						continue
					}
					if _, exists := seen[normalized]; exists {
						continue
					}
					seen[normalized] = struct{}{}
					result = append(result, normalized)
				}
			}
		}
	}
	return result
}

func normalizeExtension(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, ".")
	return strings.ToLower(trimmed)
}

func containsExtension(list []string, ext string) bool {
	for _, item := range list {
		if item == ext {
			return true
		}
	}
	return false
}

func compareCollectionSortKey(a, b *collectionPayload) bool {
	keyA := normalizeCollectionSortKey(a)
	keyB := normalizeCollectionSortKey(b)
	if keyA == keyB {
		return strings.Compare(a.DisplayName, b.DisplayName) < 0
	}
	return keyA < keyB
}

func normalizeCollectionSortKey(coll *collectionPayload) string {
	if coll == nil {
		return ""
	}
	if strings.TrimSpace(coll.SortKey) != "" {
		return strings.ToLower(coll.SortKey)
	}
	return strings.ToLower(coll.Name)
}

func compareGameSortKey(a, b *gamePayload) bool {
	keyA := normalizeSortKey(a)
	keyB := normalizeSortKey(b)
	if keyA == keyB {
		return strings.Compare(a.DisplayName, b.DisplayName) < 0
	}
	return keyA < keyB
}

func normalizeSortKey(game *gamePayload) string {
	if game == nil {
		return ""
	}
	if game.SortKey != "" {
		return strings.ToLower(game.SortKey)
	}
	for _, field := range game.Fields {
		if field == nil {
			continue
		}
		if field.Key == "sort-by" && len(field.Values) > 0 {
			return strings.ToLower(field.Values[0])
		}
	}
	return strings.ToLower(game.DisplayName)
}

func (c *WebCommand) updateGameMetadata(metadataPath string, xIndexID int, fields []*fieldPayload, removed []*fieldPayload) error {
	doc, err := metadata.ParseMetadataFile(metadataPath)
	if err != nil {
		return err
	}
	block, _, err := findGameBlockByIndexID(doc, xIndexID)
	if err != nil {
		return err
	}
	fields, err = c.materializeStagedFields(metadataPath, doc, block, fields)
	if err != nil {
		return err
	}
	if err := c.handleRemovedFields(metadataPath, doc, block, removed); err != nil {
		return err
	}
	order, updates, err := combineFieldValues(fields, "game")
	if err != nil {
		return err
	}
	if err := validateRequiredGameFields(updates); err != nil {
		return err
	}
	entries, err := rebuildBlockEntries(block.Entries, order, updates, "game")
	if err != nil {
		return err
	}
	block.Entries = entries
	return metadata.WriteMetadataFile(metadataPath, doc)
}

func (c *WebCommand) createGame(metadataPath string, xIndexID int, fields []*fieldPayload) (int, error) {
	doc, err := metadata.ParseMetadataFile(metadataPath)
	if err != nil {
		return 0, err
	}
	if _, _, err := findGameBlockByIndexID(doc, xIndexID); err == nil {
		return 0, fmt.Errorf("x-index-id %d already exists", xIndexID)
	}
	if xIndexID <= 0 {
		xIndexID = nextGameIndexID(doc)
	}
	blk := &metadata.Block{Kind: metadata.KindGame}
	doc.Blocks = append(doc.Blocks, blk)
	setBlockXIndexID(blk, xIndexID)
	fields, err = c.materializeStagedFields(metadataPath, doc, blk, fields)
	if err != nil {
		return 0, err
	}
	order, updates, err := combineFieldValues(fields, "game")
	if err != nil {
		return 0, err
	}
	if err := validateRequiredGameFields(updates); err != nil {
		return 0, err
	}
	entries, err := rebuildBlockEntries(blk.Entries, order, updates, "game")
	if err != nil {
		return 0, err
	}
	blk.Entries = entries
	if err := metadata.WriteMetadataFile(metadataPath, doc); err != nil {
		return 0, err
	}
	return xIndexID, nil
}

func nextGameIndexID(doc *metadata.Document) int {
	maxID := 0
	if doc == nil {
		return 1
	}
	for _, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindGame {
			continue
		}
		if entry := blk.Entry(xIndexEntryKey); entry != nil {
			if id, ok := parseXIndexEntry(entry); ok && id > maxID {
				maxID = id
			}
		}
	}
	return maxID + 1
}

func (c *WebCommand) deleteGame(metadataPath string, xIndexID int, removeFiles bool) error {
	doc, err := metadata.ParseMetadataFile(metadataPath)
	if err != nil {
		return err
	}
	block, idx, err := findGameBlockByIndexID(doc, xIndexID)
	if err != nil {
		return err
	}
	if removeFiles {
		c.removeGameFiles(metadataPath, doc, block)
	}
	doc.Blocks = append(doc.Blocks[:idx], doc.Blocks[idx+1:]...)
	return metadata.WriteMetadataFile(metadataPath, doc)
}

func findGameBlockByIndexID(doc *metadata.Document, xIndexID int) (*metadata.Block, int, error) {
	if doc == nil {
		return nil, -1, errors.New("metadata document is empty")
	}
	for idx, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindGame {
			continue
		}
		if blockXIndexID(blk) == xIndexID {
			return blk, idx, nil
		}
	}
	return nil, -1, fmt.Errorf("game with x-index-id %d not found", xIndexID)
}

func findCollectionBlockByIndexID(doc *metadata.Document, xIndexID int) (*metadata.Block, int, error) {
	if doc == nil {
		return nil, -1, errors.New("metadata document is empty")
	}
	for idx, blk := range doc.Blocks {
		if blk == nil || blk.Kind != metadata.KindCollection {
			continue
		}
		if blockXIndexID(blk) == xIndexID {
			return blk, idx, nil
		}
	}
	return nil, -1, fmt.Errorf("collection with x-index-id %d not found", xIndexID)
}

func (c *WebCommand) updateCollectionMetadata(metadataPath string, xIndexID int, fields []*fieldPayload) error {
	doc, err := metadata.ParseMetadataFile(metadataPath)
	if err != nil {
		return err
	}
	block, _, err := findCollectionBlockByIndexID(doc, xIndexID)
	if err != nil {
		return err
	}
	order, updates, err := combineFieldValues(fields, "collection")
	if err != nil {
		return err
	}
	if err := validateRequiredCollectionFields(updates); err != nil {
		return err
	}
	entries, err := rebuildBlockEntries(block.Entries, order, updates, "collection")
	if err != nil {
		return err
	}
	block.Entries = entries
	return metadata.WriteMetadataFile(metadataPath, doc)
}

func combineFieldValues(fields []*fieldPayload, requiredKey string) ([]string, map[string][]string, error) {
	order := make([]string, 0, len(fields))
	values := make(map[string][]string)
	hasRequired := false
	requiredKey = strings.ToLower(strings.TrimSpace(requiredKey))
	for _, field := range fields {
		if field == nil {
			continue
		}
		rawKey := strings.TrimSpace(field.Key)
		if rawKey == "" {
			return nil, nil, fmt.Errorf("field key cannot be empty")
		}
		key := strings.ToLower(rawKey)
		normalized := normalizeFieldValuesForKey(key, field.Values)
		if len(normalized) == 0 {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = append(values[key], normalized...)
		if requiredKey != "" && key == requiredKey {
			hasRequired = true
		}
	}
	if requiredKey != "" && !hasRequired {
		return nil, nil, fmt.Errorf("%s entry is required", requiredKey)
	}
	return order, values, nil
}

func rebuildBlockEntries(original []*metadata.Entry, order []string, updates map[string][]string, requiredKey string) ([]*metadata.Entry, error) {
	entries := make([]*metadata.Entry, 0, len(original)+len(updates))
	used := make(map[string]bool)
	requiredKey = strings.ToLower(strings.TrimSpace(requiredKey))
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
		if key == requiredKey && requiredKey != "" {
			return nil, fmt.Errorf("%s entry is required", requiredKey)
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
	requiredIdx := -1
	for idx, entry := range entries {
		if entry != nil && entry.Key == requiredKey && requiredKey != "" {
			requiredIdx = idx
			break
		}
	}
	if requiredKey != "" {
		if requiredIdx == -1 {
			return nil, fmt.Errorf("%s entry is required", requiredKey)
		}
		if requiredIdx != 0 {
			entries[0], entries[requiredIdx] = entries[requiredIdx], entries[0]
		}
	}
	return entries, nil
}

func validateRequiredGameFields(fields map[string][]string) error {
	if len(trimAndFilter(fields["game"])) == 0 {
		return errors.New("game field is required")
	}
	fileValues := trimAndFilter(fields["file"])
	if len(fileValues) == 0 {
		fileValues = trimAndFilter(fields["files"])
	}
	if len(fileValues) == 0 {
		return errors.New("file field is required")
	}
	if len(trimAndFilter(fields["assets.boxfront"])) == 0 {
		return errors.New("assets.boxFront field is required")
	}
	return nil
}

func validateRequiredCollectionFields(fields map[string][]string) error {
	if len(trimAndFilter(fields["collection"])) == 0 {
		return errors.New("collection field is required")
	}
	return nil
}

func trimAndFilter(values []string) []string {
	var out []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstFileValueFromFields(fields []*fieldPayload) string {
	for _, field := range fields {
		if field == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(field.Key))
		if key != "file" && key != "files" {
			continue
		}
		for _, value := range field.Values {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func (c *WebCommand) materializeStagedFields(metadataPath string, doc *metadata.Document, block *metadata.Block, fields []*fieldPayload) ([]*fieldPayload, error) {
	if c == nil || c.uploadDir == "" {
		return fields, nil
	}
	pendingFile := firstFileValueFromFields(fields)
	for _, field := range fields {
		if field == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(field.Key))
		for idx, value := range field.Values {
			if !strings.HasPrefix(value, stagedUploadPrefix) {
				continue
			}
			rel, err := c.finalizeStagedFile(metadataPath, doc, block, key, value, pendingFile)
			if err != nil {
				return nil, err
			}
			field.Values[idx] = rel
		}
	}
	return fields, nil
}

func (c *WebCommand) handleRemovedFields(metadataPath string, doc *metadata.Document, block *metadata.Block, removed []*fieldPayload) error {
	if len(removed) == 0 {
		return nil
	}
	for _, field := range removed {
		if field == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(field.Key))
		if key == "" {
			continue
		}
		for _, value := range field.Values {
			if strings.HasPrefix(value, stagedUploadPrefix) && c.uploadDir != "" {
				stagedName := strings.TrimPrefix(value, stagedUploadPrefix)
				if stagedName != "" {
					_ = os.Remove(filepath.Join(c.uploadDir, filepath.FromSlash(stagedName)))
				}
				continue
			}
			c.deleteFieldFile(metadataPath, doc, block, key, value)
		}
	}
	return nil
}

func (c *WebCommand) removeGameFiles(metadataPath string, doc *metadata.Document, block *metadata.Block) {
	if block == nil {
		return
	}
	for _, entry := range block.Entries {
		if entry == nil {
			continue
		}
		key := strings.ToLower(entry.Key)
		switch {
		case key == "file" || key == "files":
			for _, value := range entry.Values {
				c.deleteFieldFile(metadataPath, doc, block, key, value)
			}
		case strings.HasPrefix(key, "assets."):
			for _, value := range entry.Values {
				c.deleteFieldFile(metadataPath, doc, block, key, value)
			}
		}
	}
}

func (c *WebCommand) deleteFieldFile(metadataPath string, doc *metadata.Document, block *metadata.Block, key, value string) {
	metadataDir := filepath.Dir(metadataPath)
	target := filepath.Join(metadataDir, filepath.FromSlash(value))
	target = filepath.Clean(target)
	if !strings.HasPrefix(target, metadataDir+string(os.PathSeparator)) {
		return
	}
	switch {
	case strings.HasPrefix(key, "assets."):
		mediaDir := filepath.Join(metadataDir, "media") + string(os.PathSeparator)
		if strings.HasPrefix(target, mediaDir) {
			_ = os.Remove(target)
		}
	case key == "file" || key == "files":
		allowed := allowedExtensionsForGame(doc, block)
		if len(allowed) == 0 || containsExtension(allowed, normalizeExtension(filepath.Ext(target))) {
			_ = os.Remove(target)
		}
	}
}

func (c *WebCommand) finalizeStagedFile(metadataPath string, doc *metadata.Document, block *metadata.Block, key, token, pendingFile string) (string, error) {
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
	switch {
	case strings.HasPrefix(key, "assets."):
		return moveFileToMedia(metadataPath, block, pendingFile, source, stagedName, key)
	case key == "file" || key == "files":
		allowed := allowedExtensionsForGame(doc, block)
		return moveFileToRom(metadataPath, source, stagedName, allowed)
	default:
		return "", fmt.Errorf("field %s does not support uploads", key)
	}
}

func normalizeFieldValueForDisplay(key, value string) string {
	if isMultilineTextKey(key) {
		return decodeEscapedNewlines(value)
	}
	return value
}

func normalizeFieldValuesForKey(key string, values []string) []string {
	normalized := normalizeFieldValues(values)
	if len(normalized) == 0 {
		return nil
	}
	if isMultilineTextKey(key) {
		joined := strings.Join(normalized, "\n")
		escaped := encodeNewlines(joined)
		if strings.TrimSpace(escaped) == "" {
			return nil
		}
		return []string{escaped}
	}
	return normalized
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

func encodeNewlines(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\\n")
}

func decodeEscapedNewlines(value string) string {
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "\\n", "\n")
}

func isMultilineTextKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "description", "summary", "desc":
		return true
	default:
		return false
	}
}

func moveFileToMedia(metadataPath string, block *metadata.Block, pendingFile string, sourcePath, stagedName, assetKey string) (string, error) {
	metadataDir := filepath.Dir(metadataPath)
	romBase := deriveRomBase(extractBlockFiles(block))
	if romBase == "" {
		romBase = deriveRomBaseFromValue(pendingFile)
	}
	if romBase == "" {
		romBase = sanitizeFileComponent(getBlockTitle(block))
	}
	mediaDir := filepath.Join(metadataDir, "media")
	if romBase != "" {
		mediaDir = filepath.Join(mediaDir, romBase)
	}
	targetBase := assetFileBaseFromKey(assetKey)
	return moveFileToDir(metadataDir, mediaDir, sourcePath, stagedName, nil, targetBase)
}

func moveFileToRom(metadataPath string, sourcePath, stagedName string, allowedExt []string) (string, error) {
	metadataDir := filepath.Dir(metadataPath)
	return moveFileToDir(metadataDir, metadataDir, sourcePath, stagedName, allowedExt, "")
}

func moveFileToDir(metadataDir, targetDir, sourcePath, stagedName string, allowedExt []string, preferredBase string) (string, error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	baseName := stagedName
	if idx := strings.Index(baseName, "__"); idx >= 0 && idx+2 < len(baseName) {
		baseName = baseName[idx+2:]
	}
	if baseName == "" {
		baseName = filepath.Base(sourcePath)
	}
	if preferredBase != "" {
		ext := filepath.Ext(baseName)
		base := sanitizeFileComponent(preferredBase)
		if base != "" {
			baseName = base + ext
		}
	}
	if len(allowedExt) > 0 {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(baseName)), ".")
		if ext == "" || !containsExtension(allowedExt, ext) {
			return "", fmt.Errorf("file extension %s not allowed", ext)
		}
	}
	destPath := filepath.Join(targetDir, baseName)
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

func deriveRomBaseFromValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, stagedUploadPrefix) {
		value = filepath.Base(strings.TrimPrefix(value, stagedUploadPrefix))
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	base := path.Base(normalized)
	ext := path.Ext(base)
	base = strings.TrimSuffix(base, ext)
	if idx := strings.Index(base, "__"); idx >= 0 {
		start := idx + 2
		if start > len(base) {
			start = len(base)
		}
		base = base[start:]
	}
	return sanitizeFileComponent(base)
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
		normalized := strings.ReplaceAll(trimmed, "\\", string(os.PathSeparator))
		if filepath.IsAbs(normalized) {
			return filepath.ToSlash(filepath.Clean(normalized))
		}
		joined := filepath.Join(baseDir, normalized)
		return filepath.ToSlash(filepath.Clean(joined))
	}
	return ""
}

func findExistingRomPath(baseDir string, files []string) string {
	for _, file := range files {
		candidate := resolveRomPath(baseDir, []string{file})
		if candidate != "" && romFileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func romFileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(filepath.FromSlash(path))
	return err == nil
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

func collectGameAssets(metadataDir string, game metadata.Game, romBase string, romMissing bool, store *assetStore, logger *zap.Logger) ([]*assetPayload, map[string]fallbackAssetField) {
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
			if !romMissing { //rom不存在, 那么就没必要打这个日志了, 总不能rom不存在, 但是media存在吧...
				logger.Warn("skip asset", zap.String("game", game.Title), zap.String("asset", candidate.name), zap.String("path", candidate.path), zap.Error(err))
			}
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

func assetFileBaseFromKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if !strings.HasPrefix(key, "assets.") {
		return ""
	}
	name := strings.TrimPrefix(key, "assets.")
	switch name {
	case "boxfront":
		return "boxFront"
	case "boxback":
		return "boxBack"
	case "boxspine":
		return "boxSpine"
	case "boxfull":
		return "boxFull"
	case "marquee":
		return "marquee"
	case "bezel":
		return "bezel"
	case "logo":
		return "logo"
	case "screenshot":
		return "screenshot"
	case "video":
		return "video"
	case "cartridge":
		return "cartridge"
	case "disc":
		return "disc"
	case "cart":
		return "cart"
	default:
		return sanitizeFileComponent(name)
	}
}

func init() {
	RegisterRunner("web", func() IRunner { return NewWebCommand() })
}
