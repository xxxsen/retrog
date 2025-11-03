package app

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var (
	sanitizeRegexp          = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)
	whitespaceCollapseRegex = regexp.MustCompile(`\s+`)
	repeatPunctRegex        = regexp.MustCompile(`([[:punct:]])([[:punct:]])+`)
	nonNameCharRegex        = regexp.MustCompile(`[^\p{L}\p{N}-]+`)
	hyphenCollapseRegex     = regexp.MustCompile(`-+`)
)

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

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for sha1 %s: %w", path, err)
	}
	defer f.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
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
