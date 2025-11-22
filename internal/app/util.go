package app

import (
	"strings"
	"unicode"
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

func isGameNamePrefixRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
