package app

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var sanitizeRegexp = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func fileMD5(path string) (string, error) {
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

func s3Path(bucket, key string) string {
	return fmt.Sprintf("s3://%s/%s", bucket, key)
}

func parseS3Path(path string) (string, string, error) {
	if path == "" {
		return "", "", fmt.Errorf("empty s3 path")
	}
	if !strings.HasPrefix(path, "s3://") {
		return "", "", fmt.Errorf("invalid s3 path %s", path)
	}
	trimmed := strings.TrimPrefix(path, "s3://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid s3 path %s", path)
	}
	return parts[0], parts[1], nil
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	name = sanitizeRegexp.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._-")
	if name == "" {
		return "unknown"
	}
	return name
}
