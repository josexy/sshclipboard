package naming

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// docTables returns the rows of each markdown table under each "## "
// section of a doc, cells unescaped ("\|" is "|") and trimmed.
func docTables(t *testing.T, path string) map[string][][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][][]string{}
	section := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
			continue
		}
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|--") || strings.HasPrefix(line, "|-") {
			continue
		}
		line = strings.ReplaceAll(line, `\|`, "\x00")
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, strings.TrimSpace(strings.ReplaceAll(c, "\x00", "|")))
		}
		out[section] = append(out[section], cells)
	}
	return out
}

var codeSpan = regexp.MustCompile("`([^`]*)`")

func spans(cell string) []string {
	var out []string
	for _, m := range codeSpan.FindAllStringSubmatch(cell, -1) {
		out = append(out, m[1])
	}
	return out
}

func docTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04:05.000", s, time.Local)
	if err != nil {
		at, err = time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	}
	if err != nil {
		t.Fatalf("time %q: %v", s, err)
	}
	return at
}

func docEntry(t *testing.T, cell string, at time.Time) Vars {
	t.Helper()
	switch cell {
	case "image":
		return Vars{Now: at, Name: "image", Ext: "png", Kind: "image"}
	case "text":
		return Vars{Now: at, Name: "text", Ext: "txt", Kind: "text"}
	}
	s := spans(cell)
	if len(s) != 1 {
		t.Fatalf("entry %q", cell)
	}
	name, ext := s[0], ""
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		name, ext = s[0][:i], s[0][i+1:]
	}
	return Vars{Now: at, Name: name, Ext: ext, Kind: "file"}
}

// Task 4.5: every example in docs/naming.md is a test case.
func TestDocsNamingExamples(t *testing.T) {
	tables := docTables(t, "../../docs/naming.md")
	checked := 0
	at := docTime(t, "2026-11-12 15:30:45")

	// The defaults table at the top.
	for _, row := range tables[""][1:] {
		entry := map[string]string{"Images": "image", "Files": "`Quarterly Report.pdf`", "Text": "text"}[row[0]]
		got := render(t, spans(row[1])[0], docEntry(t, entry, at), Transforms{})
		if want := spans(row[2])[0]; got != want {
			t.Errorf("defaults %s: got %q, want %q", row[0], got, want)
		}
		checked++
	}

	for _, row := range tables["Examples"][1:] {
		v := docEntry(t, row[1], docTime(t, row[2]))
		var tr Transforms
		for _, opt := range strings.Split(row[3], ",") {
			k, val, _ := strings.Cut(strings.TrimSpace(opt), "=")
			switch k {
			case "":
			case "counter":
				n, _ := strconv.Atoi(val)
				v.Counter = func() int { return n }
			case "hash":
				v.Hash = val
			case "profile":
				v.Profile = val
			case "host":
				v.Host = val
			case "lowercase":
				tr.Lowercase = true
			case "underscores":
				tr.SpacesToUnderscore = true
			default:
				t.Fatalf("unknown option %q", opt)
			}
		}
		got := render(t, spans(row[0])[0], v, tr)
		if want := spans(row[4])[0]; got != want {
			t.Errorf("example %s: got %q, want %q", row[0], got, want)
		}
		checked++
	}

	for _, row := range tables["Safe names"][1:] {
		s := spans(row[0])
		ext := ""
		if len(s) > 1 {
			ext = strings.TrimPrefix(s[1], ".")
		}
		if got, want := Sanitize(s[0], ext, at), spans(row[1])[0]; got != want {
			t.Errorf("safe name %q: got %q, want %q", s[0], got, want)
		}
		checked++
	}

	for _, row := range tables["Directories"][1:] {
		v := Vars{Now: docTime(t, row[1])}
		if strings.Contains(row[0], "(an image)") {
			v.Kind = "image"
		}
		got, err := RenderDir(spans(row[0])[0], v)
		if row[2] == "rejected" {
			if err == nil {
				t.Errorf("directory %s: rendered %q, want rejected", row[0], got)
			}
		} else if want := spans(row[2])[0]; err != nil || got != want {
			t.Errorf("directory %s: got %q %v, want %q", row[0], got, err, want)
		}
		checked++
	}
	if checked < 20 {
		t.Errorf("checked only %d examples; did the doc's tables change shape?", checked)
	}

	// The prose examples.
	if got := render(t, "img_{datetime}", docEntry(t, "image", at), Transforms{}); !strings.HasSuffix(got, ".png") {
		t.Errorf("img_{datetime} gave %q", got)
	}
	v := docEntry(t, "image", at)
	v.Hash = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got := render(t, "{hash:8}-{rand:4}.{ext}", v, Transforms{}); !regexp.MustCompile(`^9f86d081-[a-z0-9]{4}\.png$`).MatchString(got) {
		t.Errorf("{hash:8}-{rand:4}.{ext} gave %q", got)
	}
	if got := render(t, "{rand}", Vars{}, Transforms{}); len(got) != 6 {
		t.Errorf("{rand} default length: %q", got)
	}
	if got := render(t, "{counter}", Vars{Counter: func() int { return 7 }}, Transforms{}); got != "007" {
		t.Errorf("{counter} default padding: %q", got)
	}
}
