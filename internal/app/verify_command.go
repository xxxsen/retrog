package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"retrog/internal/config"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// VerifyResult captures duplicate and collision information discovered during verification.
type VerifyResult struct {
	RomDuplicates   []DuplicateGroup
	RomCollisions   []DuplicateGroup
	MediaDuplicates []DuplicateGroup
	MediaCollisions []DuplicateGroup
}

// DuplicateGroup represents a set of files sharing the same MD5 hash.
type DuplicateGroup struct {
	MD5   string
	Files []FileSignature
}

// FileSignature tracks hashes for a single file.
type FileSignature struct {
	Path string
	SHA1 string
}

// VerifyCommand performs duplicate detection and stores the result.
type VerifyCommand struct {
	rootDir string
	result  *VerifyResult
}

// NewVerifyCommand constructs a verify command instance.
func NewVerifyCommand() *VerifyCommand {
	return &VerifyCommand{}
}

// SetConfig is present for interface compatibility (verify has no config requirements).
func (c *VerifyCommand) SetConfig(cfg *config.Config) {
	// no-op; verify doesn't require configuration
}

// Init registers CLI flags that affect the command.
func (c *VerifyCommand) Init(fst *pflag.FlagSet) {
	fst.StringVar(&c.rootDir, "dir", "", "ROM root directory to verify")
}

// PreRun performs validation and setup.
func (c *VerifyCommand) PreRun(ctx context.Context) error {
	if c.rootDir == "" {
		return errors.New("verify requires --dir")
	}
	logutil.GetLogger(ctx).Info("starting verify", zap.String("dir", c.rootDir))
	return nil
}

// Run executes the verify command.
func (c *VerifyCommand) Run(ctx context.Context) error {
	res, err := verify(ctx, c.rootDir)
	if err != nil {
		return err
	}
	c.result = res
	return nil
}

// PostRun emits summary details after execution.
func (c *VerifyCommand) PostRun(ctx context.Context) error {
	if c.result != nil {
		logutil.GetLogger(ctx).Info("verify summary",
			zap.Int("rom_duplicates", len(c.result.RomDuplicates)),
			zap.Int("rom_collisions", len(c.result.RomCollisions)),
			zap.Int("media_duplicates", len(c.result.MediaDuplicates)),
			zap.Int("media_collisions", len(c.result.MediaCollisions)),
		)
	}
	return nil
}

// Result returns the verification outcome gathered during Run.
func (c *VerifyCommand) Result() *VerifyResult {
	return c.result
}

func verify(ctx context.Context, root string) (*VerifyResult, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("verify root must be a directory: %s", root)
	}

	romHashes := make(map[string][]FileSignature)
	mediaHashes := make(map[string][]FileSignature)

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		md5sum, err := fileMD5(path)
		if err != nil {
			return err
		}
		sha1sum, err := fileSHA1(path)
		if err != nil {
			return err
		}

		entry := FileSignature{Path: path, SHA1: sha1sum}

		if isMediaFile(path) {
			mediaHashes[md5sum] = append(mediaHashes[md5sum], entry)
		} else {
			romHashes[md5sum] = append(romHashes[md5sum], entry)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	result := &VerifyResult{}
	result.RomDuplicates, result.RomCollisions = classifyDuplicates(romHashes)
	result.MediaDuplicates, result.MediaCollisions = classifyDuplicates(mediaHashes)

	return result, nil
}

func classifyDuplicates(data map[string][]FileSignature) (dupes []DuplicateGroup, collisions []DuplicateGroup) {
	for md5sum, entries := range data {
		if len(entries) < 2 {
			continue
		}

		shaSet := make(map[string]struct{})
		for _, entry := range entries {
			shaSet[entry.SHA1] = struct{}{}
		}

		group := DuplicateGroup{MD5: md5sum, Files: entries}
		if len(shaSet) > 1 {
			collisions = append(collisions, group)
		} else {
			dupes = append(dupes, group)
		}
	}
	return dupes, collisions
}

func isMediaFile(path string) bool {
	slashed := filepath.ToSlash(path)
	return strings.Contains(slashed, "/media/")
}
