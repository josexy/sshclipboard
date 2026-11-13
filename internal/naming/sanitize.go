package naming

import (
	"strings"
	"time"
	"unicode/utf8"
)

// MaxNameBytes is the longest filename Linux filesystems take, in bytes.
const MaxNameBytes = 255

// unsafeRune tells whether r is replaced by "_" in names: path separators,
// NUL and control characters, and characters shells or Windows tools
// treat specially.
func unsafeRune(r rune) bool {
	switch r {
	case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
		return true
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == utf8.RuneError
}

// sanitizeSegment applies the character rules to one path segment: unsafe
// characters become "_", leading and trailing spaces and dots are trimmed,
// a leading "-" becomes "_", and it is shortened to MaxNameBytes keeping
// ext ("" for none) at its end.
func sanitizeSegment(s, ext string) string {
	s = strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return '_'
		}
		return r
	}, strings.ToValidUTF8(s, "_"))
	s = strings.Trim(s, " .")
	if strings.HasPrefix(s, "-") {
		s = "_" + s[1:]
	}
	if len(s) <= MaxNameBytes {
		return s
	}
	suffix := ""
	if ext != "" && strings.HasSuffix(s, "."+ext) && len(ext)+1 < MaxNameBytes {
		suffix = "." + ext
	} else if i := strings.LastIndexByte(s, '.'); i > 0 && len(s)-i <= 32 {
		suffix = s[i:]
	}
	base := s[:len(s)-len(suffix)]
	limit := MaxNameBytes - len(suffix)
	// Cut at a rune boundary.
	for limit > 0 && !utf8.RuneStart(base[limit]) {
		limit--
	}
	return strings.TrimRight(base[:limit], " .") + suffix
}

// Sanitize makes a rendered filename safe for Linux filesystems and
// shells: "/", "\", NUL, control characters and : * ? " < > | become "_";
// leading and trailing spaces and dots are trimmed; a leading "-" becomes
// "_"; and the base name is shortened so the name is at most 255 bytes
// with ext kept. An empty result becomes clipboard_<datetime>.<ext>.
func Sanitize(name, ext string, now time.Time) string {
	s := sanitizeSegment(name, ext)
	if s == "" {
		s = "clipboard_" + formatDate(fmtDatetime, now)
		if ext != "" {
			s += "." + ext
		}
		s = sanitizeSegment(s, ext)
	}
	return s
}
