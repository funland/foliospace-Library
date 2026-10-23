package domain

import (
	"regexp"
	"strings"
	"unicode"
)

var creatorByPrefix = regexp.MustCompile(`(?i)^by(?:\s+|:\s*)`)
var creatorSpacedSeparator = regexp.MustCompile(`\s+[&/]\s+`)

// CreatorDisplayName formats a label only. Raw imported/manual values remain
// the source of truth. Commas and words such as "and" are intentionally not
// split because they can be part of an author's name or surname-first spelling.
func CreatorDisplayName(value string) string {
	value = strings.TrimSpace(value)
	value = creatorByPrefix.ReplaceAllString(value, "")
	value = strings.TrimRightFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，;；、|/&", r)
	})
	value = creatorSpacedSeparator.ReplaceAllString(value, ";")
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return strings.ContainsRune(";；、|", r)
	})
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			labels = append(labels, part)
		}
	}
	return strings.Join(labels, " · ")
}
