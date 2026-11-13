package rules

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/store"
)

func docSize(t *testing.T, s string) int64 {
	t.Helper()
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	mult := int64(1)
	if strings.HasSuffix(s, "MB") {
		s, mult = strings.TrimSuffix(s, "MB"), store.MB
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("size %q", s)
	}
	return n * mult
}

// docRule parses "skip kind=image maxwidth=31" and the like.
func docRule(t *testing.T, id int64, s string) store.Rule {
	t.Helper()
	fields := strings.Fields(s)
	r := store.Rule{ID: id, Name: "rule " + strconv.FormatInt(id, 10), Enabled: true, Action: store.Action(fields[0])}
	for _, f := range fields[1:] {
		k, v, _ := strings.Cut(f, "=")
		c := &r.Conditions
		switch k {
		case "kind":
			c.Kinds = append(c.Kinds, store.Kind(v))
		case "ext":
			c.Extensions = strings.Split(v, ",")
		case "glob":
			c.Glob = v
		case "min":
			n := docSize(t, v)
			c.MinSize = &n
		case "max":
			n := docSize(t, v)
			c.MaxSize = &n
		case "maxwidth":
			n, _ := strconv.Atoi(v)
			c.MaxWidth = &n
		case "regex":
			c.TextRegex = v
		case "dir":
			r.DirOverride = v
		default:
			t.Fatalf("unknown condition %q", f)
		}
	}
	return r
}

var (
	docCode  = regexp.MustCompile("`([^`]*)`")
	docImage = regexp.MustCompile(`^image (\d+)×(\d+)$`)
)

func docEntry(t *testing.T, cell string) Entry {
	t.Helper()
	if m := docImage.FindStringSubmatch(cell); m != nil {
		w, _ := strconv.Atoi(m[1])
		h, _ := strconv.Atoi(m[2])
		return image(w, h)
	}
	code := docCode.FindStringSubmatch(cell)
	if code == nil {
		t.Fatalf("entry %q", cell)
	}
	rest := strings.TrimSpace(docCode.ReplaceAllString(cell, ""))
	switch {
	case strings.HasPrefix(cell, "text "):
		return text(code[1])
	case strings.HasPrefix(cell, "file "):
		size := int64(1000)
		if r := strings.TrimSpace(strings.TrimPrefix(rest, "file")); r != "" {
			size = docSize(t, r)
		}
		return file(code[1], size)
	}
	t.Fatalf("entry %q", cell)
	return Entry{}
}

// Task 5.4: the examples in docs/rules.md are test cases.
func TestDocsRulesExamples(t *testing.T) {
	b, err := os.ReadFile("../../docs/rules.md")
	if err != nil {
		t.Fatal(err)
	}
	_, examples, ok := strings.Cut(string(b), "## Examples")
	if !ok {
		t.Fatal("no Examples section")
	}
	v := naming.Vars{Now: time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)}
	checked := 0
	for _, line := range strings.Split(examples, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		var list []store.Rule
		for i, rs := range strings.Split(docCode.FindStringSubmatch(cells[0])[1], ";") {
			list = append(list, docRule(t, int64(i+1), strings.TrimSpace(rs)))
		}
		set, errs := Compile(list, store.Action(cells[1]))
		if len(errs) > 0 {
			t.Fatalf("%s: %v", cells[0], errs)
		}
		entry := docEntry(t, cells[2])
		d := set.Evaluate(entry)

		parts := strings.Split(cells[3], ", ")
		wantAction, wantRule := store.Action(parts[0]), parts[1]
		got := "default action"
		if !d.Default() {
			got = "rule " + strconv.FormatInt(d.RuleID, 10)
		}
		if d.Action != wantAction || got != wantRule {
			t.Errorf("%s with %s: got %s by %s, want %s by %s", cells[0], cells[2], d.Action, got, wantAction, wantRule)
		}
		if len(parts) > 2 {
			v.Kind = string(entry.Kind)
			dir, err := d.Dir("~/clipboard", v)
			if want := docCode.FindStringSubmatch(parts[2])[1]; err != nil || dir != want {
				t.Errorf("%s: folder %q %v, want %q", cells[0], dir, err, want)
			}
		}
		checked++
	}
	if checked < 10 {
		t.Errorf("checked %d examples", checked)
	}
}
