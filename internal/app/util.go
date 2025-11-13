package app

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xxxsen/common/logutil"
	appdb "github.com/xxxsen/retrog/internal/db"
	"go.uber.org/zap"
)

const largeFileHashThreshold int64 = 10 * 1024 * 1024

var (
	whitespaceCollapseRegex = regexp.MustCompile(`\s+`)
	repeatPunctRegex        = regexp.MustCompile(`([[:punct:]])([[:punct:]])+`)
	nonNameCharRegex        = regexp.MustCompile(`[^\p{L}\p{N}-]+`)
	hyphenCollapseRegex     = regexp.MustCompile(`-+`)
)

func readFileMD5WithCache(ctx context.Context, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat file for hash %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("hash file %s: is a directory", path)
	}

	if info.Size() <= largeFileHashThreshold {
		return computeFileMD5(path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	cleanPath := filepath.Clean(absPath)
	modTime := info.ModTime().UnixMilli()

	hash, ok, err := appdb.FileHashCacheDao.Lookup(ctx, cleanPath, modTime)
	if err != nil {
		return "", err
	}
	if ok {
		logutil.GetLogger(ctx).Debug("read file hash from cache", zap.String("file", path), zap.String("hash", hash))
		return hash, nil
	}
	logutil.GetLogger(ctx).Info("big file cache compute", zap.String("file", path), zap.Int64("size", info.Size()))
	hash, err = computeFileMD5(path)
	if err != nil {
		return "", err
	}

	if err := appdb.FileHashCacheDao.Upsert(ctx, cleanPath, modTime, hash); err != nil {
		return "", err
	}

	return hash, nil
}

func computeFileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for hash %s: %w", path, err)
	}
	defer f.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func cleanDescription(desc string) string {
	if desc == "" {
		return desc
	}
	desc = normalizeWidth(desc)
	desc = repeatPunctRegex.ReplaceAllString(desc, `$1`)
	lines := strings.Split(desc, "\n")
	for i, line := range lines {
		lines[i] = whitespaceCollapseRegex.ReplaceAllString(line, " ")
	}
	return strings.Join(lines, "\n")
}

func cleanGameName(name string) string {
	name = normalizeWidth(name)
	name = whitespaceCollapseRegex.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = nonNameCharRegex.ReplaceAllString(name, "")
	name = hyphenCollapseRegex.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "unknown"
	}
	return name
}

func normalizeWidth(input string) string {
	if input == "" {
		return input
	}

	var b strings.Builder
	b.Grow(len(input))

	for _, r := range input {
		switch {
		case r == 0x3000:
			b.WriteRune(' ')
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

func readerMD5(r io.Reader) (string, error) {
	hasher := md5.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", fmt.Errorf("hash reader: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func mediaRelativePath(hash, ext string) string {
	h := hash
	if len(h) < 2 {
		h += strings.Repeat("0", 2-len(h))
	}
	first := h[:2]
	return filepath.Join("media", first, hash+ext)
}
