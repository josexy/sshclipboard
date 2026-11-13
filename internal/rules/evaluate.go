package rules

import (
	"path"
	"strings"

	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/store"
)

// Entry is one upload entry as rules see it: the image of an image item,
// the text of a text item, or one file of a files item.
type Entry struct {
	Kind store.Kind
	// FileName is the original file name with its extension; image and
	// text entries use "image.png" and "text.txt" (see EntryFor).
	FileName string
	Size     int64
	Width    int // images only
	Height   int // images only
	Text     string
}

// Ext returns the entry's extension without the dot, lower-cased.
func (e Entry) Ext() string {
	return normExt(path.Ext(e.FileName))
}

// PseudoName returns the name rules and naming use for an image or text
// entry: "image.png" and "text.txt". Files keep their own names.
func PseudoName(kind store.Kind) string {
	switch kind {
	case store.KindImage:
		return "image.png"
	case store.KindText:
		return "text.txt"
	}
	return ""
}

// Decision is the outcome of evaluating an entry.
type Decision struct {
	// RuleName is the matching rule's name, or store.RuleDefault when no
	// rule matched.
	RuleName         string
	RuleID           int64 // 0 for the default action
	Action           store.Action
	DirOverride      string
	TemplateOverride string
}

// Default tells whether no rule matched.
func (d Decision) Default() bool { return d.RuleID == 0 }

// Evaluate returns the decision for e: the first enabled rule, top to
// bottom, whose set conditions all match, else the default action.
func (s *Set) Evaluate(e Entry) Decision {
	for _, c := range s.rules {
		if c.rule.Enabled && c.matches(e) {
			d := Decision{RuleName: c.rule.Name, RuleID: c.rule.ID, Action: c.rule.Action}
			if c.rule.Action == store.ActionUpload {
				d.DirOverride, d.TemplateOverride = c.rule.DirOverride, c.rule.TemplateOverride
			}
			return d
		}
	}
	return Decision{RuleName: store.RuleDefault, Action: s.defaultAction}
}

// matches reports whether every set condition matches e. A condition that
// only applies to some kinds (a text regex, image dimensions) does not
// match entries of other kinds.
func (c compiled) matches(e Entry) bool {
	cond := c.rule.Conditions
	if len(c.kinds) > 0 && !c.kinds[e.Kind] {
		return false
	}
	if len(c.exts) > 0 && !c.exts[e.Ext()] {
		return false
	}
	if c.glob != "" {
		if ok, _ := path.Match(c.glob, strings.ToLower(e.FileName)); !ok {
			return false
		}
	}
	if cond.MinSize != nil && e.Size < *cond.MinSize {
		return false
	}
	if cond.MaxSize != nil && e.Size > *cond.MaxSize {
		return false
	}
	dims := cond.MinWidth != nil || cond.MaxWidth != nil || cond.MinHeight != nil || cond.MaxHeight != nil
	if dims {
		if e.Kind != store.KindImage {
			return false
		}
		if (cond.MinWidth != nil && e.Width < *cond.MinWidth) || (cond.MaxWidth != nil && e.Width > *cond.MaxWidth) ||
			(cond.MinHeight != nil && e.Height < *cond.MinHeight) || (cond.MaxHeight != nil && e.Height > *cond.MaxHeight) {
			return false
		}
	}
	if c.re != nil {
		if e.Kind != store.KindText || !c.re.MatchString(e.Text) {
			return false
		}
	}
	return true
}

// Dir resolves the entry's remote directory: the profile's directory
// template, with the decision's override (absolute, "~/", or relative to
// the profile's directory) when it has one. Both are rendered with v.
func (d Decision) Dir(profileDir string, v naming.Vars) (string, error) {
	return ResolveDir(profileDir, d.DirOverride, v)
}

// ResolveDir renders the profile's directory template and an override,
// and joins them as Decision.Dir does.
func ResolveDir(profileDir, override string, v naming.Vars) (string, error) {
	base, err := naming.RenderDir(profileDir, v)
	if err != nil {
		return "", err
	}
	if override == "" {
		return base, nil
	}
	o, err := naming.RenderDir(override, v)
	if err != nil {
		return "", err
	}
	return naming.JoinDir(base, o), nil
}
