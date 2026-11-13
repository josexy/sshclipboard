package naming

import (
	"fmt"
	"strconv"
	"strings"
)

// Token names.
const (
	TokName     = "name"
	TokExt      = "ext"
	TokDate     = "date"
	TokTime     = "time"
	TokDatetime = "datetime"
	TokUnix     = "unix"
	TokCounter  = "counter"
	TokRand     = "rand"
	TokHash     = "hash"
	TokKind     = "kind"
	TokProfile  = "profile"
	TokHost     = "host"
)

// tokenSpec says what a token accepts.
type tokenSpec struct {
	arg      argKind
	def      int // default N for argN tokens
	min, max int // valid N
	inDir    bool
}

type argKind int

const (
	argNone argKind = iota
	argN
	argFormat
)

var tokens = map[string]tokenSpec{
	TokName:     {inDir: false},
	TokExt:      {inDir: false},
	TokDate:     {arg: argFormat, inDir: true},
	TokTime:     {inDir: true},
	TokDatetime: {inDir: true},
	TokUnix:     {inDir: true},
	TokCounter:  {arg: argN, def: 3, min: 1, max: 9},
	TokRand:     {arg: argN, def: 6, min: 1, max: 32},
	TokHash:     {arg: argN, def: 8, min: 1, max: 64},
	TokKind:     {inDir: true},
	TokProfile:  {inDir: true},
	TokHost:     {inDir: true},
}

// ParseError is a template error at a position: the 1-based character
// (rune) index where the problem starts.
type ParseError struct {
	Pos   int
	Token string // the offending token as written, e.g. "{nmae}"; "" for brace errors
	Kind  ErrorKind
	Msg   string
}

// ErrorKind classifies a ParseError, for the view to translate.
type ErrorKind int

const (
	ErrUnknownToken ErrorKind = iota + 1
	ErrUnbalanced
	ErrBadArgument
	ErrNoArgument // the token takes no argument
	ErrNotAllowed // the token is not allowed in directory templates
	ErrTraversal  // a ".." directory segment
	ErrEmptyTemplate
)

func (e *ParseError) Error() string {
	if e.Token != "" {
		return fmt.Sprintf("%s at position %d: %s", e.Token, e.Pos, e.Msg)
	}
	return fmt.Sprintf("position %d: %s", e.Pos, e.Msg)
}

// part is a literal or a token of a parsed template.
type part struct {
	lit    string
	tok    string // "" for a literal
	n      int    // N of counter, rand, hash
	format []datePart
}

// Template is a parsed filename or directory template.
type Template struct {
	src   string
	parts []part
	uses  map[string]bool
}

// Source returns the template as written.
func (t *Template) Source() string { return t.src }

// Uses tells whether the template contains a token.
func (t *Template) Uses(tok string) bool { return t.uses[tok] }

// Parse parses a filename template. It rejects unknown tokens, unbalanced
// braces, and invalid FORMAT or N arguments, with the error's position.
func Parse(src string) (*Template, error) { return parse(src, false) }

func parse(src string, dir bool) (*Template, error) {
	t := &Template{src: src, uses: map[string]bool{}}
	if strings.TrimSpace(src) == "" {
		return nil, &ParseError{Pos: 1, Kind: ErrEmptyTemplate, Msg: "the template is empty"}
	}
	runes := []rune(src)
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			t.parts = append(t.parts, part{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '{' && i+1 < len(runes) && runes[i+1] == '{':
			lit.WriteRune('{')
			i++
		case r == '}' && i+1 < len(runes) && runes[i+1] == '}':
			lit.WriteRune('}')
			i++
		case r == '}':
			return nil, &ParseError{Pos: i + 1, Kind: ErrUnbalanced, Msg: `unexpected "}" (write "}}" for a literal brace)`}
		case r == '{':
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '}' {
					end = j
					break
				}
				if runes[j] == '{' {
					return nil, &ParseError{Pos: i + 1, Kind: ErrUnbalanced, Msg: `"{" is not closed (write "{{" for a literal brace)`}
				}
			}
			if end < 0 {
				return nil, &ParseError{Pos: i + 1, Kind: ErrUnbalanced, Msg: `"{" is not closed (write "{{" for a literal brace)`}
			}
			p, err := parseToken(string(runes[i+1:end]), i+1, dir)
			if err != nil {
				return nil, err
			}
			flush()
			t.parts = append(t.parts, p)
			t.uses[p.tok] = true
			i = end
		default:
			lit.WriteRune(r)
		}
	}
	flush()
	return t, nil
}

func parseToken(body string, pos int, dir bool) (part, error) {
	written := "{" + body + "}"
	name, arg, hasArg := strings.Cut(body, ":")
	spec, ok := tokens[name]
	if !ok {
		return part{}, &ParseError{Pos: pos, Token: written, Kind: ErrUnknownToken, Msg: "unknown token"}
	}
	if dir && !spec.inDir {
		return part{}, &ParseError{Pos: pos, Token: written, Kind: ErrNotAllowed, Msg: "this token is not allowed in a directory"}
	}
	p := part{tok: name}
	switch spec.arg {
	case argNone:
		if hasArg {
			return part{}, &ParseError{Pos: pos, Token: written, Kind: ErrNoArgument, Msg: "this token takes no argument"}
		}
	case argN:
		p.n = spec.def
		if hasArg {
			n, err := strconv.Atoi(arg)
			if err != nil || n < spec.min || n > spec.max || strings.TrimSpace(arg) != arg {
				return part{}, &ParseError{Pos: pos, Token: written, Kind: ErrBadArgument,
					Msg: fmt.Sprintf("N must be a number from %d to %d", spec.min, spec.max)}
			}
			p.n = n
		}
	case argFormat:
		format := "YYYY-MM-DD"
		if hasArg {
			format = arg
		}
		f, bad, err := parseDateFormat(format)
		if err != nil {
			return part{}, &ParseError{Pos: pos + len([]rune(name)) + 2 + bad, Token: written, Kind: ErrBadArgument, Msg: err.Error()}
		}
		p.format = f
	}
	return p, nil
}
