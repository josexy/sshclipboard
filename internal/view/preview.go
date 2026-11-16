package view

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"

	"github.com/josexy/sshclipboard/internal/store"
)

const (
	// previewTextLimit is the most of a text entry a preview shows.
	previewTextLimit = 1 << 20
	// previewMaxSide is the longest side of an image preview's bitmap, in
	// pixels: larger images are scaled down to it, which keeps the bitmap
	// within GPU texture limits and 64 MiB of pixels.
	previewMaxSide = 4096
)

// previewContent is what a preview shows of a text or image entry.
type previewContent struct {
	kind   store.Kind
	bitmap *ui.Bitmap     // an image's, scaled down to previewMaxSide
	text   *ui.TextBuffer // a text's, at most previewTextLimit bytes of it
	// width and height are an image's size in pixels before scaling.
	width, height int
	// size is the staged file's size in bytes.
	size int64
	// truncated tells that text shows only its first previewTextLimit
	// bytes.
	truncated bool
	// unavailable tells that the staged file is missing or unreadable.
	unavailable bool
}

// readPreview reads an entry's staged file. It reads files and decodes
// images, so it runs off the UI thread.
func readPreview(e store.HistoryEntry) previewContent {
	if e.Kind == store.KindImage {
		return readPreviewImage(e.LocalPath)
	}
	return readPreviewText(e.LocalPath)
}

// readPreviewText reads the first previewTextLimit bytes of a staged text,
// cut back to a whole UTF-8 sequence, with invalid bytes replaced.
func readPreviewText(path string) previewContent {
	pc := previewContent{kind: store.KindText}
	f, err := os.Open(path)
	if err != nil {
		pc.unavailable = true
		return pc
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		pc.unavailable = true
		return pc
	}
	// The bytes past the limit tell that there is more.
	data, err := io.ReadAll(io.LimitReader(f, previewTextLimit+utf8.UTFMax-1))
	if err != nil {
		pc.unavailable = true
		return pc
	}
	pc.size = max(st.Size(), int64(len(data)))
	if len(data) > previewTextLimit {
		data = cutUTF8(data[:previewTextLimit])
		pc.truncated = true
	}
	pc.text = ui.NewTextBuffer(strings.ToValidUTF8(string(data), "�"))
	return pc
}

// cutUTF8 drops a UTF-8 sequence that b, cut from longer text, ends in the
// middle of. Invalid bytes stay, as the replacement character shows them.
func cutUTF8(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

// readPreviewImage decodes a staged PNG, scaled down to previewMaxSide on
// its longest side.
func readPreviewImage(path string) previewContent {
	pc := previewContent{kind: store.KindImage}
	data, err := os.ReadFile(path)
	if err != nil {
		pc.unavailable = true
		return pc
	}
	pc.size = int64(len(data))
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		pc.unavailable = true
		return pc
	}
	pc.width, pc.height = cfg.Width, cfg.Height
	if max(cfg.Width, cfg.Height) <= previewMaxSide {
		if pc.bitmap, err = ui.DecodeBitmap(data); err != nil {
			pc.unavailable = true
		}
		return pc
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		pc.unavailable = true
		return pc
	}
	// ApproxBiLinear is as capture's thumbnails: over ten times faster than
	// CatmullRom, and as smooth while an image is less than twice the
	// limit, as an 8K screenshot is.
	w, h := scaleToSide(cfg.Width, cfg.Height, previewMaxSide)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	pc.bitmap = ui.NewBitmap(dst)
	return pc
}

// scaleToSide scales w×h down so that its longest side is side, keeping
// its aspect ratio.
func scaleToSide(w, h, side int) (int, int) {
	if w >= h {
		return side, max(1, (h*side+w/2)/w)
	}
	return max(1, (w*side+h/2)/h), side
}

// previewKey names what a preview shows: an entry's staged file.
type previewKey struct {
	id   int64
	path string
}

// previewLoader reads the preview of one entry at a time off the UI
// thread, as thumbCache does thumbnails: get returns false until the
// content is ready, then redraw brings it in. It holds the content of the
// entry wanted last alone, and drops results for others as they arrive.
// Its methods and redraw's functions run on the UI thread.
type previewLoader struct {
	redraw func(func())
	// read reads an entry's content; tests replace it.
	read func(store.HistoryEntry) previewContent

	wants    bool // an entry is wanted
	wanted   previewKey
	has      bool // content is wanted's
	content  previewContent
	inflight map[previewKey]bool
}

func newPreviewLoader(redraw func(func())) *previewLoader {
	return &previewLoader{redraw: redraw, read: readPreview, inflight: map[previewKey]bool{}}
}

// get returns e's content once it is ready. Until then it reads it in a
// goroutine, unless one is reading it already, and returns false.
func (l *previewLoader) get(e store.HistoryEntry) (previewContent, bool) {
	k := previewKey{e.ID, e.LocalPath}
	if !l.wants || k != l.wanted {
		l.wants, l.wanted, l.has, l.content = true, k, false, previewContent{}
	}
	if l.has {
		return l.content, true
	}
	if !l.inflight[k] && l.redraw != nil && l.read != nil {
		l.inflight[k] = true
		read, redraw := l.read, l.redraw
		go func() {
			pc := read(e)
			redraw(func() { l.done(k, pc) })
		}()
	}
	return previewContent{}, false
}

// done takes the content read for k, if k is still wanted.
func (l *previewLoader) done(k previewKey, pc previewContent) {
	delete(l.inflight, k)
	if l.wants && k == l.wanted && !l.has {
		l.content, l.has = pc, true
	}
}

// reset forgets the entry wanted and drops its content.
func (l *previewLoader) reset() {
	l.wants, l.wanted, l.has, l.content = false, previewKey{}, false, previewContent{}
}
