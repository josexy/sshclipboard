package core

import (
	"path"
	"strconv"
	"strings"

	"github.com/josexy/sshclipboard/internal/store"
)

// CopyBackTokens are the tokens of the clipboard text template.
var CopyBackTokens = []string{"{path}", "{dir}", "{name}", "{user}", "{host}", "{port}", "{profile}"}

// CopyBackText builds the text copied back after a capture uploaded: the
// profile's template for each remote path, joined by its separator.
// {path}, {dir}, and {name} are shell-quoted when the profile says so and
// they need it.
func CopyBackText(p store.Profile, paths []string) string {
	tmpl := p.CopyTemplate
	if tmpl == "" {
		tmpl = "{path}"
	}
	sep := p.CopySeparator
	if sep == "" {
		sep = " "
	}
	quote := func(s string) string { return s }
	if p.ShellQuote {
		quote = ShellQuote
	}
	parts := make([]string, len(paths))
	for i, full := range paths {
		parts[i] = strings.NewReplacer(
			"{path}", quote(full),
			"{dir}", quote(path.Dir(full)),
			"{name}", quote(path.Base(full)),
			"{user}", p.Username,
			"{host}", p.Host,
			"{port}", strconv.Itoa(p.Port),
			"{profile}", p.Name,
		).Replace(tmpl)
	}
	return strings.Join(parts, sep)
}

// ShellQuote wraps s in single quotes when a POSIX shell would split or
// expand it, escaping embedded single quotes as '\”. Safe strings are
// returned as they are.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./-_", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ValidateCopyTemplate reports the first unknown {token} of a clipboard
// template, or "" when there is none.
func ValidateCopyTemplate(tmpl string) string {
	for rest := tmpl; ; {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			return ""
		}
		j := strings.IndexByte(rest[i:], '}')
		if j < 0 {
			return rest[i:]
		}
		tok := rest[i : i+j+1]
		known := false
		for _, t := range CopyBackTokens {
			known = known || t == tok
		}
		if !known {
			return tok
		}
		rest = rest[i+j+1:]
	}
}
