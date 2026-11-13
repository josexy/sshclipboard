package naming

import (
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Vars are the values tokens render from.
type Vars struct {
	Now     time.Time // the entry's local capture time
	Name    string    // original base name without extension ("image", "text")
	Ext     string    // original extension without the dot ("png", "txt")
	Hash    string    // hex SHA-256 of the content
	Kind    string    // "image", "file", "text"
	Profile string
	Host    string
	// Counter returns the next {counter} value. It is called at most once
	// per render, and only when the template uses {counter}, so the
	// per-day sequence only advances for names that show it.
	Counter func() int
	// Rand is the source of {rand}; nil means crypto/rand.
	Rand io.Reader
}

// Transforms are a template's optional name transforms, applied after
// rendering and before sanitizing.
type Transforms struct {
	Lowercase          bool
	SpacesToUnderscore bool
}

const randAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandString returns n lowercase alphanumeric characters from r (nil:
// crypto/rand).
func RandString(r io.Reader, n int) string {
	if r == nil {
		r = rand.Reader
	}
	out := make([]byte, 0, n)
	buf := make([]byte, 1)
	for len(out) < n {
		if _, err := io.ReadFull(r, buf); err != nil {
			panic(fmt.Sprintf("naming: random source: %v", err))
		}
		// Rejection sampling keeps the 36 characters equally likely.
		if buf[0] < 252 {
			out = append(out, randAlphabet[buf[0]%36])
		}
	}
	return string(out)
}

// Render renders the template, without transforms or sanitizing.
func (t *Template) Render(v Vars) string {
	return t.render(v, func(s string) string { return s })
}

// render renders with each token value passed through esc.
func (t *Template) render(v Vars, esc func(string) string) string {
	var b strings.Builder
	counter := -1
	for _, p := range t.parts {
		if p.tok == "" {
			b.WriteString(p.lit)
			continue
		}
		var s string
		switch p.tok {
		case TokName:
			s = v.Name
		case TokExt:
			s = v.Ext
		case TokDate:
			s = formatDate(p.format, v.Now)
		case TokTime:
			s = formatDate(fmtTime, v.Now)
		case TokDatetime:
			s = formatDate(fmtDatetime, v.Now)
		case TokUnix:
			s = strconv.FormatInt(v.Now.Unix(), 10)
		case TokCounter:
			if counter < 0 {
				counter = 1
				if v.Counter != nil {
					counter = v.Counter()
				}
			}
			s = fmt.Sprintf("%0*d", p.n, counter)
		case TokRand:
			s = RandString(v.Rand, p.n)
		case TokHash:
			s = v.Hash
			if len(s) > p.n {
				s = s[:p.n]
			}
		case TokKind:
			s = v.Kind
		case TokProfile:
			s = v.Profile
		case TokHost:
			s = v.Host
		}
		b.WriteString(esc(s))
	}
	return b.String()
}

// RenderName renders a filename: the template, then ".ext" when the
// template has no {ext} and the entry has an extension, then the
// transforms, then Sanitize.
func RenderName(t *Template, v Vars, tr Transforms) string {
	s := t.Render(v)
	if !t.Uses(TokExt) && v.Ext != "" {
		s += "." + v.Ext
	}
	if tr.Lowercase {
		s = strings.ToLower(s)
	}
	if tr.SpacesToUnderscore {
		s = strings.ReplaceAll(s, " ", "_")
	}
	ext := v.Ext
	if tr.Lowercase {
		ext = strings.ToLower(ext)
	}
	return Sanitize(s, ext, v.Now)
}

// SampleVars returns the sample entry the live preview renders for kind:
// image.png, example.txt for files, and text.txt.
func SampleVars(kind, profile, host string, now time.Time) Vars {
	v := Vars{Now: now, Kind: kind, Profile: profile, Host: host,
		Hash:    "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		Counter: func() int { return 1 },
		Rand:    strings.NewReader(strings.Repeat("\x00\x01\x02\x03\x04\x05\x06\x07", 8)),
	}
	switch kind {
	case "image":
		v.Name, v.Ext = "image", "png"
	case "text":
		v.Name, v.Ext = "text", "txt"
	default:
		v.Name, v.Ext = "example", "txt"
	}
	return v
}
