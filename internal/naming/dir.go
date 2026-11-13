package naming

import (
	"strings"
)

// DirTemplate is a parsed remote directory template: a profile's default
// directory or a rule's directory override. It accepts {date}, {time},
// {datetime}, {date:FORMAT}, {unix}, {kind}, {profile}, and {host}.
type DirTemplate struct {
	t *Template
}

// Source returns the template as written.
func (d *DirTemplate) Source() string { return d.t.src }

// ParseDir parses a directory template. Besides Parse's errors, it rejects
// tokens that are not allowed in directories and any ".." segment.
func ParseDir(src string) (*DirTemplate, error) {
	t, err := parse(src, true)
	if err != nil {
		return nil, err
	}
	d := &DirTemplate{t: t}
	// Literal ".." segments are rejected up front; token values cannot
	// make one because Render escapes their slashes.
	if _, err := d.Render(Vars{}); err != nil {
		return nil, err
	}
	return d, nil
}

// Render renders the directory: tokens are substituted (with any "/" or
// "\" in their values made "_", so a value cannot add segments), "/"
// separates segments, a ".." segment is an error, "." and empty segments
// are dropped, and each segment is sanitized like a filename. A leading
// "/" (absolute) and a leading "~" segment (the home directory) are kept.
func (d *DirTemplate) Render(v Vars) (string, error) {
	raw := d.t.render(v, func(s string) string {
		return strings.NewReplacer("/", "_", "\\", "_").Replace(s)
	})
	return cleanDir(raw, d.t.src)
}

func cleanDir(raw, src string) (string, error) {
	absolute := strings.HasPrefix(raw, "/")
	var segs []string
	for i, seg := range strings.Split(raw, "/") {
		trimmed := strings.TrimSpace(seg)
		switch {
		case trimmed == "..":
			return "", &ParseError{Pos: segmentPos(src, ".."), Kind: ErrTraversal,
				Msg: `".." is not allowed in a directory`}
		case trimmed == "" || trimmed == ".":
			continue
		case i == 0 && trimmed == "~":
			segs = append(segs, "~")
			continue
		}
		if s := sanitizeSegment(seg, ""); s != "" {
			segs = append(segs, s)
		}
	}
	out := strings.Join(segs, "/")
	switch {
	case absolute:
		return "/" + out, nil
	case out == "":
		return ".", nil
	}
	return out, nil
}

func segmentPos(src, seg string) int {
	if i := strings.Index(src, seg); i >= 0 {
		return len([]rune(src[:i])) + 1
	}
	return 1
}

// RenderDir parses and renders a directory template in one step.
func RenderDir(src string, v Vars) (string, error) {
	d, err := ParseDir(src)
	if err != nil {
		return "", err
	}
	return d.Render(v)
}

// JoinDir resolves a rule's directory override against the profile's
// directory: an absolute override or one starting with "~/" (or exactly
// "~") replaces it, and any other is relative to it. Both are rendered
// directories.
func JoinDir(profileDir, override string) string {
	switch {
	case override == "" || override == ".":
		return profileDir
	case strings.HasPrefix(override, "/") || override == "~" || strings.HasPrefix(override, "~/"):
		return override
	case profileDir == "/":
		return "/" + override
	case profileDir == "" || profileDir == ".":
		return override
	}
	return strings.TrimSuffix(profileDir, "/") + "/" + override
}
