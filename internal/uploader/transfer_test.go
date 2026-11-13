package uploader

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
)

// 7.1 Remote paths and directories.

// Spec "Home-relative directory": history gets the absolute path.
func TestHomeRelativeDirectory(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	src := localFile(t, "shot.png", []byte("png"))
	for i, c := range []struct{ dir, want string }{
		{"~/clipboard", "/home/alice/clipboard/shot.png"},
		{"inbox", "/home/alice/inbox/shot.png"},
		{"/srv/in", "/srv/in/shot.png"},
	} {
		id := int64(i + 1)
		e.u.Enqueue(e.job(id, src, c.dir, "shot.png"))
		ev := e.rec.final(t, id)
		if ev.Kind != Done || ev.RemotePath != c.want {
			t.Errorf("%s: %+v", c.dir, ev)
		}
		if got := e.remote(t, c.want); string(got) != "png" {
			t.Errorf("%s: content %q", c.dir, got)
		}
	}
}

// Spec "Dated folder created".
func TestCreateMissingDirectories(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	j := e.job(1, localFile(t, "a.txt", []byte("a")), "~/clipboard/2026-11", "a.txt")
	e.u.Enqueue(j)
	if ev := e.rec.final(t, 1); ev.Kind != Done || ev.RemotePath != "/home/alice/clipboard/2026-11/a.txt" {
		t.Errorf("%+v", ev)
	}
}

func TestMissingDirectoryWithoutCreate(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	j := e.job(1, localFile(t, "a.txt", []byte("a")), "~/nope", "a.txt")
	j.CreateDirs = false
	e.u.Enqueue(j)
	ev := e.rec.final(t, 1)
	if ev.Kind != Failed || ev.Attempt != 1 || !errors.Is(ev.Err, sshconn.ErrRemoteDirMissing) ||
		!strings.Contains(ev.Err.Error(), "remote directory does not exist") {
		t.Errorf("%+v", ev)
	}
}

// Spec "File deleted before upload".
func TestSourceFileNotFound(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	e.u.Enqueue(e.job(1, "/no/such/temp.log", "~", "temp.log"))
	ev := e.rec.final(t, 1)
	if ev.Kind != Failed || ev.Attempt != 1 || !errors.Is(ev.Err, ErrSourceNotFound) {
		t.Errorf("%+v", ev)
	}
}

// 7.2 Atomic writes, progress, and mode.

// Spec "Connection lost mid-transfer": no file has the final name, and
// the next attempt starts again.
func TestNoFinalFileAfterDrop(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 500_000) // 5 MB
	src := localFile(t, "big.bin", data)

	e := newEnv(t, Options{Retries: NoRetries}, nil)
	e.s.DropAfter(2_000_000) // 40%
	e.u.Enqueue(e.job(1, src, "~/in", "big.bin"))
	ev := e.rec.final(t, 1)
	if ev.Kind != Failed {
		t.Fatalf("%+v", ev)
	}
	if _, err := os.Stat(e.s.Local("/home/alice/in/big.bin")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a partial file has the final name: %v", err)
	}

	// With retries, the next attempt sends everything again and removes
	// the first attempt's temporary file.
	e2 := newEnv(t, Options{}, nil)
	e2.s.DropAfter(2_000_000)
	e2.u.Enqueue(e2.job(1, src, "~/in", "big.bin"))
	ev = e2.rec.final(t, 1)
	if ev.Kind != Done || ev.Attempt != 2 {
		t.Fatalf("%+v", ev)
	}
	if got := e2.remote(t, "/home/alice/in/big.bin"); !bytes.Equal(got, data) {
		t.Errorf("uploaded %d bytes, want %d", len(got), len(data))
	}
	if p := e2.parts("/home/alice/in"); len(p) != 0 {
		t.Errorf("temporary files left: %v", p)
	}
}

// Spec "Mode applied".
func TestFileMode(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	j := e.job(1, localFile(t, "a.txt", []byte("a")), "~", "a.txt")
	j.FileMode = 0o640
	e.u.Enqueue(j)
	const remote = "/home/alice/a.txt"
	if ev := e.rec.final(t, 1); ev.Kind != Done || ev.RemotePath != remote {
		t.Fatalf("%+v", ev)
	}
	// The SFTP request carries the exact POSIX mode even when the test
	// server runs on Windows, where the backing filesystem reports 0666.
	changes := e.s.ChmodRequests()
	if len(changes) != 1 || changes[0].Mode != 0o640 || path.Dir(changes[0].Path) != path.Dir(remote) ||
		!strings.HasSuffix(changes[0].Path, ".part") {
		t.Errorf("chmod requests %+v, want mode 0640 on the temporary upload", changes)
	}
	fi, err := os.Stat(e.s.Local(remote))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); runtime.GOOS != "windows" && got != 0o640 {
		t.Errorf("remote file mode %04o, want 0640", got)
	}
}

func TestTemporaryFileIsHiddenPart(t *testing.T) {
	p := partName("shot.png")
	if !strings.HasPrefix(p, ".shot.png.") || !strings.HasSuffix(p, ".part") || len(p) != len(".shot.png.abcdef.part") {
		t.Errorf("part name %q", p)
	}
}

// Progress is reported at most 4 times per second.
func TestProgressThrottled(t *testing.T) {
	clock := time.Unix(0, 0)
	var reports []int64
	pr := &progressReader{ctx: bg, r: &slowReader{left: 100 * 1024}, total: 100 * 1024, every: 250 * time.Millisecond,
		now:    func() time.Time { clock = clock.Add(100 * time.Millisecond); return clock },
		report: func(sent, total int64, speed float64) { reports = append(reports, sent) }}
	buf := make([]byte, 1024)
	for {
		if _, err := pr.Read(buf); err != nil {
			break
		}
	}
	// 101 reads over 10.1 s of clock: one report per 250 ms at most.
	if len(reports) > 41 || len(reports) < 30 {
		t.Errorf("%d reports in 10 s", len(reports))
	}
}

func TestProgressEventsHaveSpeed(t *testing.T) {
	e := newEnv(t, Options{ProgressInterval: 20 * time.Millisecond, Open: slowSource(2*1024*1024, 5*time.Millisecond)}, nil)
	e.u.Enqueue(e.job(1, "x", "~", "big.bin"))
	if ev := e.rec.final(t, 1); ev.Kind != Done {
		t.Fatalf("%+v", ev)
	}
	var withSpeed int
	for _, ev := range e.rec.all() {
		if ev.Kind == Progress {
			if ev.Total != 2*1024*1024 || ev.Sent > ev.Total {
				t.Errorf("progress %+v", ev)
			}
			if ev.Speed > 0 {
				withSpeed++
			}
		}
	}
	if withSpeed == 0 {
		t.Error("no progress event had a speed")
	}
}

// 7.3 Conflict policies.

// Spec "Rename on conflict".
func TestConflictRename(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	os.WriteFile(e.s.Local("/home/alice/report.pdf"), []byte("old"), 0o644)
	os.WriteFile(e.s.Local("/home/alice/report-1.pdf"), []byte("old1"), 0o644)
	e.u.Enqueue(e.job(1, localFile(t, "report.pdf", []byte("new")), "~", "report.pdf"))
	ev := e.rec.final(t, 1)
	if ev.Kind != Done || ev.RemotePath != "/home/alice/report-2.pdf" {
		t.Fatalf("%+v", ev)
	}
	if string(e.remote(t, "/home/alice/report.pdf")) != "old" || string(e.remote(t, "/home/alice/report-2.pdf")) != "new" {
		t.Error("contents")
	}
}

// Spec "Overwrite".
func TestConflictOverwrite(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	os.WriteFile(e.s.Local("/home/alice/notes.txt"), []byte("old"), 0o644)
	j := e.job(1, localFile(t, "notes.txt", []byte("new content")), "~", "notes.txt")
	j.Policy = store.ConflictOverwrite
	e.u.Enqueue(j)
	if ev := e.rec.final(t, 1); ev.Kind != Done || ev.RemotePath != "/home/alice/notes.txt" {
		t.Fatalf("%+v", ev)
	}
	if got := e.remote(t, "/home/alice/notes.txt"); string(got) != "new content" {
		t.Errorf("content %q", got)
	}
}

// Skip: "already exists", and nothing is transferred.
func TestConflictSkip(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	os.WriteFile(e.s.Local("/home/alice/a.txt"), []byte("old"), 0o644)
	j := e.job(1, localFile(t, "a.txt", []byte("new")), "~", "a.txt")
	j.Policy = store.ConflictSkip
	e.u.Enqueue(j)
	ev := e.rec.final(t, 1)
	if ev.Kind != Skipped || !errors.Is(ev.Err, ErrExists) || ev.RemotePath != "/home/alice/a.txt" {
		t.Fatalf("%+v", ev)
	}
	if e.s.Written() != 0 || string(e.remote(t, "/home/alice/a.txt")) != "old" {
		t.Error("a skipped entry was transferred")
	}
}

func TestNumbered(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"report.pdf", 2, "report-2.pdf"}, {"Makefile", 1, "Makefile-1"},
		{"a.tar.gz", 3, "a.tar-3.gz"}, {".env", 1, ".env-1"},
	} {
		if got := numbered(c.in, c.n); got != c.want {
			t.Errorf("numbered(%q, %d) = %q", c.in, c.n, got)
		}
	}
}

// fakeRenamer is a server without posix-rename whose rename overwrites.
type fakeRenamer struct {
	files   map[string]string
	removed []string
}

func (f *fakeRenamer) Rename(o, n string) error {
	f.files[n] = f.files[o]
	delete(f.files, o)
	return nil
}
func (f *fakeRenamer) PosixRename(o, n string) error { return errors.New("unsupported") }
func (f *fakeRenamer) Remove(p string) error {
	if _, ok := f.files[p]; !ok {
		return fs.ErrNotExist
	}
	f.removed = append(f.removed, p)
	delete(f.files, p)
	return nil
}
func (f *fakeRenamer) Stat(p string) (fs.FileInfo, error) {
	if _, ok := f.files[p]; ok {
		return nil, nil
	}
	return nil, fs.ErrNotExist
}
func (f *fakeRenamer) HasExtension(string) (string, bool) { return "", false }

func TestCommitOnOtherServers(t *testing.T) {
	// Rename checks names first, so an overwriting rename never clobbers.
	f := &fakeRenamer{files: map[string]string{"/d/.p": "new", "/d/a.txt": "old"}}
	got, err := commit(f, "/d/.p", "/d", "a.txt", store.ConflictRename)
	if err != nil || got != "/d/a-1.txt" || f.files["/d/a.txt"] != "old" {
		t.Errorf("rename: %q %v %v", got, err, f.files)
	}
	// Overwrite without posix-rename: remove, then rename.
	f = &fakeRenamer{files: map[string]string{"/d/.p": "new", "/d/a.txt": "old"}}
	got, err = commit(f, "/d/.p", "/d", "a.txt", store.ConflictOverwrite)
	if err != nil || got != "/d/a.txt" || f.files["/d/a.txt"] != "new" || len(f.removed) != 1 {
		t.Errorf("overwrite: %q %v %v", got, err, f.files)
	}
}
