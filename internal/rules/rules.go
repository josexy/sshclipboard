package rules

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/store"
)

// Rule edit dialog fields, the Field of a FieldError.
const (
	FieldName     = "name"
	FieldGlob     = "glob"
	FieldRegex    = "text_regex"
	FieldSize     = "size"
	FieldWidth    = "width"
	FieldHeight   = "height"
	FieldDir      = "dir_override"
	FieldTemplate = "template_override"
	FieldAction   = "action"
)

// Validation problems the view translates; regex, glob, and template
// errors carry their parser's message instead.
var (
	ErrMinOverMax = errors.New("the minimum is greater than the maximum")
	ErrNegative   = errors.New("the value must not be negative")
	ErrAction     = errors.New("unknown action")
	ErrRequired   = errors.New("required")
)

// FieldError is a problem with one field of a rule.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e *FieldError) Unwrap() error { return e.Err }

// RuleError lists a rule's invalid fields.
type RuleError struct {
	RuleID int64
	Name   string
	Fields []*FieldError
}

func (e *RuleError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Error()
	}
	return fmt.Sprintf("rule %q: %s", e.Name, strings.Join(parts, "; "))
}

// Field returns the error of one field, or nil.
func (e *RuleError) Field(name string) error {
	if e == nil {
		return nil
	}
	for _, f := range e.Fields {
		if f.Field == name {
			return f.Err
		}
	}
	return nil
}

// compiled is a rule ready to evaluate.
type compiled struct {
	rule  store.Rule
	kinds map[store.Kind]bool
	exts  map[string]bool
	glob  string
	re    *regexp.Regexp
}

// Set is a compiled, ordered rule list and the default action.
type Set struct {
	rules         []compiled
	defaultAction store.Action
}

// Validate checks one rule as the edit dialog saves it. It returns nil
// when the rule is valid.
func Validate(r store.Rule) *RuleError {
	_, err := compile(r)
	return err
}

func compile(r store.Rule) (compiled, *RuleError) {
	c := compiled{rule: r, kinds: map[store.Kind]bool{}, exts: map[string]bool{}}
	var fields []*FieldError
	add := func(field string, err error) { fields = append(fields, &FieldError{field, err}) }

	if strings.TrimSpace(r.Name) == "" {
		add(FieldName, ErrRequired)
	}
	if r.Action != store.ActionUpload && r.Action != store.ActionSkip {
		add(FieldAction, ErrAction)
	}
	cond := r.Conditions
	for _, k := range cond.Kinds {
		c.kinds[k] = true
	}
	for _, e := range cond.Extensions {
		if e = normExt(e); e != "" {
			c.exts[e] = true
		}
	}
	if g := strings.TrimSpace(cond.Glob); g != "" {
		c.glob = strings.ToLower(g)
		if _, err := path.Match(c.glob, ""); err != nil {
			add(FieldGlob, err)
		}
	}
	if cond.TextRegex != "" {
		re, err := regexp.Compile(cond.TextRegex)
		if err != nil {
			add(FieldRegex, err)
		}
		c.re = re
	}
	checkRange := func(field string, lo, hi *int64) {
		switch {
		case lo != nil && *lo < 0, hi != nil && *hi < 0:
			add(field, ErrNegative)
		case lo != nil && hi != nil && *lo > *hi:
			add(field, ErrMinOverMax)
		}
	}
	checkRange(FieldSize, cond.MinSize, cond.MaxSize)
	checkRange(FieldWidth, i64p(cond.MinWidth), i64p(cond.MaxWidth))
	checkRange(FieldHeight, i64p(cond.MinHeight), i64p(cond.MaxHeight))
	if r.TemplateOverride != "" {
		if _, err := naming.Parse(r.TemplateOverride); err != nil {
			add(FieldTemplate, err)
		}
	}
	if r.DirOverride != "" {
		if _, err := naming.ParseDir(r.DirOverride); err != nil {
			add(FieldDir, err)
		}
	}
	if len(fields) > 0 {
		return c, &RuleError{RuleID: r.ID, Name: r.Name, Fields: fields}
	}
	return c, nil
}

func i64p(p *int) *int64 {
	if p == nil {
		return nil
	}
	v := int64(*p)
	return &v
}

func normExt(e string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
}

// Compile compiles rules, in their list order, with the default action.
// Invalid rules are left out of the set and reported, one error each.
func Compile(rules []store.Rule, defaultAction store.Action) (*Set, []error) {
	s := &Set{defaultAction: defaultAction}
	var errs []error
	for _, r := range rules {
		c, err := compile(r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s.rules = append(s.rules, c)
	}
	return s, errs
}
