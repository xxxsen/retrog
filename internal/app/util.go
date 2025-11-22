package app

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	whitespaceCollapseRegex = regexp.MustCompile(`\s+`)
	repeatPunctRegex        = regexp.MustCompile(`([[:punct:]])([[:punct:]])+`)
	nonNameCharRegex        = regexp.MustCompile(`[^\p{L}\p{N}-]+`)
	hyphenCollapseRegex     = regexp.MustCompile(`-+`)
)

func hasGameNamePrefix(name string) bool {
	trimmed := strings.TrimSpace(name)
	runes := []rune(trimmed)
	if len(runes) < 2 {
		return false
	}
	if !isGameNamePrefixRune(runes[0]) {
		return false
	}
	return unicode.IsSpace(runes[1])
}

func stripGameNamePrefix(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if !hasGameNamePrefix(trimmed) {
		return trimmed, false
	}
	runes := []rune(trimmed)
	body := strings.TrimLeftFunc(string(runes[2:]), unicode.IsSpace)
	return body, true
}

func isGameNamePrefixRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
