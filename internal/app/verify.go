package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Verify scans the provided directory for duplicate ROM and media files.
func Verify(ctx context.Context, root string) (*VerifyResult, error) {
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
