package capture

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/josexy/sshclipboard/internal/store"
)

// Source is the clipboard as capture reads it. The app's source wraps
// mygo.Clipboard; tests use a fake. Methods may be called from any
// goroutine.
type Source interface {
	// Formats lists the formats on the clipboard, portable names
	// ("image/png") and registered Windows names
	// ("ExcludeClipboardContentFromMonitorProcessing") alike.
	Formats() []string
	// Files returns the local paths of a copied file list.
	Files() ([]string, error)
	// Image returns the image as PNG. It fails with ErrTooLarge when the
	// representation exceeds the size limit.
	Image() ([]byte, error)
	// Text returns the plain text. It fails with ErrTooLarge as Image.
	Text() (string, error)
	// Format returns one representation's raw bytes.
	Format(name string) ([]byte, error)
}

// ErrTooLarge is a clipboard representation over the 64 MiB limit.
var ErrTooLarge = errors.New("content too large")

// MaxBytes is the largest image or text representation captured.
const MaxBytes = 64 << 20

// Clipboard format names capture looks for.
const (
	FormatFileList = "application/x-mygo-file-list"
	FormatURIList  = "text/uri-list"
	FormatPNG      = "image/png"
	FormatText     = "text/plain"

	// Formats with which password managers and other apps mark content
	// confidential on Windows.
	FormatExcludeFromMonitor = "ExcludeClipboardContentFromMonitorProcessing"
	FormatViewerIgnore       = "Clipboard Viewer Ignore"
	FormatCanIncludeHistory  = "CanIncludeInClipboardHistory"
)

// Entry is one upload entry of a captured item.
type Entry struct {
	Kind store.Kind
	// FileName is the original name with its extension: the file's own
	// name, or "image.png" / "text.txt".
	FileName  string
	LocalPath string // the staged file, or the original file
	Staged    bool
	ThumbPath string
	Size      int64
	Width     int
	Height    int
	SHA256    string // "" for files, which are not read at capture
	Text      string // text entries: the text, for rule regexes
}

// Skipped is a part of a capture recorded as skipped without upload.
type Skipped struct {
	Kind      store.Kind
	FileName  string
	LocalPath string
	Reason    string // a store.Reason* code
}

// Item is one captured clipboard change.
type Item struct {
	CaptureID  string
	Kind       store.Kind
	Trigger    store.Trigger
	CapturedAt time.Time
	// Seq is the clipboard's sequence number when the item was read, for
	// the copy-back guard.
	Seq     uint32
	Entries []Entry
	Skipped []Skipped
	// hash identifies the content, for duplicate suppression.
	hash string
}

// NewID returns a time-ordered capture ID: 13 hex digits of Unix
// milliseconds and 8 random hex digits.
func NewID(now time.Time) string {
	var b [4]byte
	rand.Read(b[:])
	return fmt.Sprintf("%013x%s", now.UnixMilli(), hex.EncodeToString(b[:]))
}
