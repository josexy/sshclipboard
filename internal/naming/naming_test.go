package naming

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 4.1 Parser.

func TestParseAcceptsEveryToken(t *testing.T) {
	for _, src := range []string{
		"{name}.{ext}", "{date}", "{time}", "{datetime}", "{date:YYYY/MM/DD_HH-mm-ss.SSS}", "{date:YY}",
		"{unix}", "{counter}", "{counter:5}", "{rand}", "{rand:4}", "{hash}", "{hash:8}", "{hash:64}",
		"{kind}", "{profile}", "{host}", "{{draft}}_{name}", "plain", "a}}b{{c", "日本{name}",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, c := range []struct {
		src   string
		kind  ErrorKind
		pos   int
		token string
	}{
		// Spec "Unknown token": the error names {nmae}.
		{"{nmae}.{ext}", ErrUnknownToken, 1, "{nmae}"},
		{"x_{Name}", ErrUnknownToken, 3, "{Name}"},
		{"{}", ErrUnknownToken, 1, "{}"},
		{"{date:QQ}", ErrBadArgument, 7, "{date:QQ}"},
		{"{date:YYYY-QQ}", ErrBadArgument, 12, "{date:YYYY-QQ}"},
		{"{date:}", ErrBadArgument, 7, "{date:}"},
		{"{counter:0}", ErrBadArgument, 1, "{counter:0}"},
		{"{counter:10}", ErrBadArgument, 1, "{counter:10}"},
		{"{rand:x}", ErrBadArgument, 1, "{rand:x}"},
		{"{hash:65}", ErrBadArgument, 1, "{hash:65}"},
		{"{hash: 8}", ErrBadArgument, 1, "{hash: 8}"},
		{"{name:3}", ErrNoArgument, 1, "{name:3}"},
		{"{time:HH}", ErrNoArgument, 1, "{time:HH}"},
		{"abc{name", ErrUnbalanced, 4, ""},
		{"{na{me}", ErrUnbalanced, 1, ""},
		{"name}", ErrUnbalanced, 5, ""},
		{"", ErrEmptyTemplate, 1, ""},
		{"   ", ErrEmptyTemplate, 1, ""},
	} {
		_, err := Parse(c.src)
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%q: got %v, want a ParseError", c.src, err)
			continue
		}
		if pe.Kind != c.kind || pe.Pos != c.pos || pe.Token != c.token {
			t.Errorf("%q: got kind %d pos %d token %q (%v), want kind %d pos %d token %q",
				c.src, pe.Kind, pe.Pos, pe.Token, pe, c.kind, c.pos, c.token)
		}
	}
	_, err := Parse("{nmae}.{ext}")
	if !strings.Contains(err.Error(), "{nmae}") || !strings.Contains(err.Error(), "unknown token") {
		t.Errorf("message %q must name {nmae} as an unknown token", err)
	}
}

func TestParseRecordsUses(t *testing.T) {
	tm, err := Parse("{counter}_{name}")
	if err != nil {
		t.Fatal(err)
	}
	if !tm.Uses(TokCounter) || !tm.Uses(TokName) || tm.Uses(TokExt) {
		t.Errorf("uses %v", tm.uses)
	}
	if tm.Source() != "{counter}_{name}" {
		t.Error(tm.Source())
	}
}

// 4.2 Date FORMAT.

func TestFormatDate(t *testing.T) {
	at := time.Date(2026, 11, 12, 9, 5, 7, 250*int(time.Millisecond), time.Local)
	for _, c := range []struct{ format, want string }{
		{"YYYY/MM/DD_HH-mm-ss.SSS", "2026/11/12_09-05-07.250"},
		{"YYYY-MM-DD", "2026-11-12"},
		{"YYYYMMDD-HHmmss", "20261112-090507"},
		{"YY", "26"},
		{"HHmmss", "090507"},
		{"SSS", "250"},
		{"DD.MM.YYYY", "12.11.2026"},
		{"1 + 2 = YYYY", "1 + 2 = 2026"}, // digits are literals, never Go layout elements
		{"__", "__"},
		{"MMMM", "1111"}, // MM twice
	} {
		got, err := FormatDate(c.format, at)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q %v, want %q", c.format, got, err, c.want)
		}
	}
	for _, bad := range []string{"QQ", "YYYY-Mon", "hh", "Y", "", "YYYYY"} {
		if _, err := FormatDate(bad, at); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// 4.3 Rendering.

var at153045 = time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)

func render(t *testing.T, src string, v Vars, tr Transforms) string {
	t.Helper()
	tm, err := Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return RenderName(tm, v, tr)
}

func TestRenderSpecScenarios(t *testing.T) {
	image := Vars{Now: at153045, Name: "image", Ext: "png", Kind: "image"}
	text := Vars{Now: at153045, Name: "text", Ext: "txt", Kind: "text"}
	for _, c := range []struct {
		name, src string
		v         Vars
		tr        Transforms
		want      string
	}{
		// Spec "Default image name".
		{"default image", "screenshot_{datetime}.{ext}", image, Transforms{}, "screenshot_20261112-153045.png"},
		// Spec "Default file name".
		{"default file", "{name}.{ext}", Vars{Now: at153045, Name: "Quarterly Report", Ext: "pdf"}, Transforms{}, "Quarterly Report.pdf"},
		{"default text", "clip_{datetime}.{ext}", text, Transforms{}, "clip_20261112-153045.txt"},
		// Spec "Custom date format": "/" is replaced as unsafe.
		{"custom date", "{date:YYYY/MM/DD_HH-mm-ss.SSS}_{name}.{ext}",
			Vars{Now: time.Date(2026, 11, 12, 9, 5, 7, 250e6, time.Local), Name: "a", Ext: "txt"}, Transforms{},
			"2026_11_12_09-05-07.250_a.txt"},
		// Spec "Literal braces".
		{"literal braces", "{{draft}}_{name}.{ext}", Vars{Now: at153045, Name: "x", Ext: "md"}, Transforms{}, "{draft}_x.md"},
		// Spec "Template without extension".
		{"ext appended", "img_{datetime}", image, Transforms{}, "img_20261112-153045.png"},
		{"no ext to append", "{name}", Vars{Now: at153045, Name: "Makefile"}, Transforms{}, "Makefile"},
		// Spec "Lowercase and underscores".
		{"transforms", "{name}.{ext}", Vars{Now: at153045, Name: "My Report", Ext: "PDF"},
			Transforms{Lowercase: true, SpacesToUnderscore: true}, "my_report.pdf"},
		{"transforms with appended ext", "{name}", Vars{Now: at153045, Name: "My Report", Ext: "PDF"},
			Transforms{Lowercase: true}, "my report.pdf"},
		// capture-rules spec "Template override".
		{"override", "{name}-{datetime}.{ext}", Vars{Now: at153045, Name: "app", Ext: "log"}, Transforms{}, "app-20261112-153045.log"},
		{"date and time", "{date}_{time}", text, Transforms{}, "2026-11-12_153045.txt"},
		{"unix kind host profile", "{unix}-{kind}-{host}-{profile}", Vars{Now: time.Unix(1700000000, 0), Kind: "file",
			Host: "10.0.0.5", Profile: "dev-box", Ext: "bin"}, Transforms{}, "1700000000-file-10.0.0.5-dev-box.bin"},
		{"hash default 8", "{hash}", Vars{Now: at153045, Hash: "0123456789abcdef", Ext: "png"}, Transforms{}, "01234567.png"},
	} {
		if got := render(t, c.src, c.v, c.tr); got != c.want {
			t.Errorf("%s: %q rendered %q, want %q", c.name, c.src, got, c.want)
		}
	}
}

// Spec "Hash and random".
func TestRenderHashAndRandom(t *testing.T) {
	v := Vars{Now: at153045, Name: "image", Ext: "png", Hash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	got := render(t, "{hash:8}-{rand:4}.{ext}", v, Transforms{})
	if len(got) != len("9f86d081-abcd.png") || !strings.HasPrefix(got, "9f86d081-") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("got %q", got)
	}
	for _, r := range got[9:13] {
		if !strings.ContainsRune(randAlphabet, r) {
			t.Errorf("rand character %q in %q", r, got)
		}
	}
	// An injected source makes it deterministic.
	v.Rand = strings.NewReader("\x00\x01\x23\xff\x24")
	if got := render(t, "{rand:4}", v, Transforms{}); got != "ab9a.png" {
		t.Errorf("injected rand %q", got) // 0xff is rejected and skipped
	}
	if s := RandString(nil, 32); len(s) != 32 {
		t.Errorf("crypto rand %q", s)
	}
}

// Spec "Counter resets daily" / "Counter survives restart": the counter
// comes from the store; rendering pads it and asks for it only when used.
func TestRenderCounter(t *testing.T) {
	calls := 0
	next := 3
	v := Vars{Now: at153045, Name: "image", Ext: "png", Counter: func() int { calls++; return next }}
	if got := render(t, "shot_{counter}.{ext}", v, Transforms{}); got != "shot_003.png" {
		t.Errorf("got %q", got)
	}
	next = 4
	if got := render(t, "shot_{counter:5}_{counter:1}.{ext}", v, Transforms{}); got != "shot_00004_4.png" {
		t.Errorf("got %q", got)
	}
	if calls != 2 {
		t.Errorf("counter called %d times, want once per render", calls)
	}
	render(t, "screenshot_{datetime}.{ext}", v, Transforms{})
	if calls != 2 {
		t.Error("a template without {counter} advanced the counter")
	}
	if got := render(t, "n{counter:2}", Vars{Now: at153045}, Transforms{}); got != "n01" {
		t.Errorf("without a counter func: %q", got)
	}
}

// file-naming spec "Live preview".
func TestPreviewSample(t *testing.T) {
	v := SampleVars("file", "dev-box", "10.0.0.5", at153045)
	if got := render(t, "{profile}_{name}.{ext}", v, Transforms{}); got != "dev-box_example.txt" {
		t.Errorf("preview %q", got)
	}
	if got := render(t, "screenshot_{datetime}.{ext}", SampleVars("image", "", "", at153045), Transforms{}); got != "screenshot_20261112-153045.png" {
		t.Errorf("image preview %q", got)
	}
	if got := render(t, "{name}.{ext}", SampleVars("text", "", "", at153045), Transforms{}); got != "text.txt" {
		t.Errorf("text preview %q", got)
	}
}

// 4.4 Sanitize and RenderDir.

func TestSanitize(t *testing.T) {
	long := strings.Repeat("a", 296) + ".png"    // 300 bytes
	longUTF8 := strings.Repeat("日", 99) + ".png" // 301 bytes
	for _, c := range []struct{ in, ext, want string }{
		// Spec "Leading dash and slash".
		{"-rf: a/b.txt", "txt", "_rf_ a_b.txt"},
		{`a\b*c?d"e<f>g|h:i.txt`, "txt", "a_b_c_d_e_f_g_h_i.txt"},
		{"tab\there\x00nul\x7f.txt", "txt", "tab_here_nul_.txt"},
		{"  ..hidden. .", "", "hidden"},
		{" name.txt ", "txt", "name.txt"},
		{"--x", "", "_-x"},
		{"ok-name_1.tar.gz", "gz", "ok-name_1.tar.gz"},
		{"日本語.txt", "txt", "日本語.txt"},
		{"bad\xffutf8.txt", "txt", "bad_utf8.txt"},
		// Empty results fall back to clipboard_<datetime>.
		{"", "png", "clipboard_20261112-153045.png"},
		{" . ", "", "clipboard_20261112-153045"},
		{"...", "txt", "clipboard_20261112-153045.txt"},
	} {
		if got := Sanitize(c.in, c.ext, at153045); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// Spec "Overlong name".
	for _, in := range []string{long, longUTF8} {
		got := Sanitize(in, "png", at153045)
		if len(got) > MaxNameBytes || !strings.HasSuffix(got, ".png") || !utf8Valid(got) {
			t.Errorf("Sanitize(%d bytes) = %d bytes %q", len(in), len(got), got)
		}
		if len(in) == 300 && len(got) != 255 {
			t.Errorf("an ASCII name should be cut to exactly 255 bytes, got %d", len(got))
		}
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }

func TestRenderNameSanitizes(t *testing.T) {
	v := Vars{Now: at153045, Name: "-rf: a/b", Ext: "txt"}
	if got := render(t, "{name}.{ext}", v, Transforms{}); got != "_rf_ a_b.txt" {
		t.Errorf("got %q", got)
	}
	if got := render(t, "{name}", Vars{Now: at153045, Ext: "png"}, Transforms{}); got != "png" {
		// "" + ".png" trims to "png": a name of only an extension is not empty.
		t.Errorf("got %q", got)
	}
}

func TestRenderDir(t *testing.T) {
	v := Vars{Now: at153045, Kind: "image", Profile: "dev-box", Host: "10.0.0.5"}
	for _, c := range []struct{ src, want string }{
		// Spec "Dated folders".
		{"~/clipboard/{date:YYYY-MM}", "~/clipboard/2026-11"},
		{"~/clipboard", "~/clipboard"},
		{"~", "~"},
		{"/srv/in/{kind}", "/srv/in/image"},
		{"screenshots/{date}", "screenshots/2026-11-12"},
		{"/srv//in/./{profile}/", "/srv/in/dev-box"},
		{"~/{host}/{datetime}", "~/10.0.0.5/20261112-153045"},
		{"~/a:b/-x/ c ", "~/a_b/_x/c"},
		{"/", "/"},
		{"~user/x", "~user/x"},
	} {
		got, err := RenderDir(c.src, v)
		if err != nil || got != c.want {
			t.Errorf("RenderDir(%q) = %q, %v; want %q", c.src, got, err, c.want)
		}
	}
	// Token values cannot add segments or climb.
	got, err := RenderDir("~/{profile}", Vars{Profile: "../etc/x"})
	if err != nil || got != "~/_etc_x" {
		t.Errorf("slashy profile: %q %v", got, err)
	}
	// A value that renders a ".." segment is rejected, as the spec rejects
	// any template that produces one.
	if got, err := RenderDir("~/{profile}/x", Vars{Profile: ".."}); err == nil {
		t.Errorf("profile '..' rendered %q", got)
	}
}

func TestParseDirRejects(t *testing.T) {
	for _, c := range []struct {
		src  string
		kind ErrorKind
	}{
		// Spec "Traversal rejected".
		{"~/clipboard/../../etc", ErrTraversal},
		{"..", ErrTraversal},
		{"a/ .. /b", ErrTraversal},
		{"~/{name}", ErrNotAllowed},
		{"~/{counter}", ErrNotAllowed},
		{"~/{ext}", ErrNotAllowed},
		{"~/{rand}", ErrNotAllowed},
		{"~/{hash}", ErrNotAllowed},
		{"~/{date:QQ}", ErrBadArgument},
		{"~/{nope}", ErrUnknownToken},
		{"~/{date", ErrUnbalanced},
	} {
		_, err := ParseDir(c.src)
		var pe *ParseError
		if !errors.As(err, &pe) || pe.Kind != c.kind {
			t.Errorf("ParseDir(%q): got %v, want kind %d", c.src, err, c.kind)
		}
	}
	_, err := ParseDir("~/clipboard/../../etc")
	var pe *ParseError
	if errors.As(err, &pe) && pe.Pos != 13 {
		t.Errorf("traversal position %d, want 13", pe.Pos)
	}
}

// capture-rules spec "Relative directory override".
func TestJoinDir(t *testing.T) {
	for _, c := range []struct{ profile, override, want string }{
		{"~/clipboard", "screenshots/2026-11-12", "~/clipboard/screenshots/2026-11-12"},
		{"~/clipboard/", "x", "~/clipboard/x"},
		{"~/clipboard", "~/shots", "~/shots"},
		{"~/clipboard", "/srv/in", "/srv/in"},
		{"~/clipboard", "", "~/clipboard"},
		{"/", "x", "/x"},
		{"~", "x", "~/x"},
	} {
		if got := JoinDir(c.profile, c.override); got != c.want {
			t.Errorf("JoinDir(%q, %q) = %q, want %q", c.profile, c.override, got, c.want)
		}
	}
}
