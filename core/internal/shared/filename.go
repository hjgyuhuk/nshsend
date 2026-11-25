package shared

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxFileNameBytes = 255

var reservedWindowsStems = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// SanitizeFileName rewrites a single untrusted path segment so it is safe on targetOS.
func SanitizeFileName(name string, targetOS string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if unicode.IsControl(r) || isIllegalFileChar(r, targetOS) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	cleaned := b.String()

	if isWindowsLikeOS(targetOS) {
		cleaned = collapseTrailingDotSpace(cleaned)
		if isReservedWindowsName(cleaned) {
			if idx := strings.IndexByte(cleaned, '.'); idx >= 0 {
				cleaned = "_" + cleaned[idx:]
			} else {
				cleaned = "_"
			}
		}
	}

	cleaned = truncateUTF8Bytes(cleaned, maxFileNameBytes)
	if isWindowsLikeOS(targetOS) {
		cleaned = collapseTrailingDotSpace(cleaned)
		cleaned = truncateUTF8Bytes(cleaned, maxFileNameBytes)
	}

	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "untitled"
	}
	return cleaned
}

// SanitizeRelativePath sanitizes an untrusted peer path, stripping traversal segments
// and keeping safe relative directories joined with OS separators.
func SanitizeRelativePath(peerPath string, targetOS string) string {
	normalized := strings.ReplaceAll(peerPath, "\\", "/")
	parts := strings.Split(normalized, "/")
	safeParts := make([]string, 0, len(parts))
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		safeParts = append(safeParts, SanitizeFileName(seg, targetOS))
	}
	if len(safeParts) == 0 {
		return "untitled"
	}
	return filepath.Join(safeParts...)
}

func isWindowsLikeOS(targetOS string) bool {
	return targetOS == "windows" || targetOS == "universal"
}

func isIllegalFileChar(r rune, targetOS string) bool {
	switch targetOS {
	case "windows", "universal":
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return true
		}
	case "darwin":
		if r == '/' || r == ':' {
			return true
		}
	default:
		if r == '/' {
			return true
		}
	}
	return false
}

func collapseTrailingDotSpace(value string) string {
	trimmed := strings.TrimRight(value, ". ")
	if len(trimmed) == len(value) {
		return value
	}
	return trimmed + "_"
}

func isReservedWindowsName(name string) bool {
	stem := name
	if idx := strings.IndexByte(name, '.'); idx >= 0 {
		stem = name[:idx]
	}
	_, ok := reservedWindowsStems[strings.ToLower(stem)]
	return ok
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
