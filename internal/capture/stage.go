package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"

	"github.com/josexy/sshclipboard/internal/store"
)

// ThumbHeight is the height of image thumbnails, in pixels.
const ThumbHeight = 96

// stager writes images and text to the staging folder.
type stager struct {
	dir func() string // the staging folder, read for each capture
}

// path returns <staging>/<yyyy-mm>/<id><suffix>, creating the folder.
func (s stager) path(id string, at time.Time, suffix string) (string, error) {
	dir := filepath.Join(s.dir(), at.Format("2006-01"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, id+suffix), nil
}

// write creates a staged file and computes its SHA-256 while writing.
func write(path string, data []byte) (string, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), bytes.NewReader(data)); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// image stages a PNG with a thumbnail and returns its entry.
func (s stager) image(id string, at time.Time, pngData []byte) (Entry, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(pngData))
	if err != nil {
		return Entry{}, fmt.Errorf("reading the image: %w", err)
	}
	p, err := s.path(id, at, ".png")
	if err != nil {
		return Entry{}, err
	}
	sum, err := write(p, pngData)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Kind: store.KindImage, FileName: "image.png", LocalPath: p, Staged: true, Size: int64(len(pngData)),
		Width: cfg.Width, Height: cfg.Height, SHA256: sum}
	// A thumbnail lets the Activity view skip decoding full screenshots;
	// without one it shows a generic icon.
	if thumb, err := thumbnail(pngData); err == nil {
		tp := filepath.Join(filepath.Dir(p), id+".thumb.png")
		if _, err := write(tp, thumb); err == nil {
			e.ThumbPath = tp
		}
	}
	return e, nil
}

// text stages UTF-8 text and returns its entry.
func (s stager) text(id string, at time.Time, text string) (Entry, error) {
	p, err := s.path(id, at, ".txt")
	if err != nil {
		return Entry{}, err
	}
	sum, err := write(p, []byte(text))
	if err != nil {
		return Entry{}, err
	}
	return Entry{Kind: store.KindText, FileName: "text.txt", LocalPath: p, Staged: true, Size: int64(len(text)),
		SHA256: sum, Text: text}, nil
}

// thumbnail scales a PNG to ThumbHeight pixels high, keeping its aspect
// ratio; smaller images keep their size.
func thumbnail(pngData []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if h > ThumbHeight {
		w = max(1, w*ThumbHeight/h)
		h = ThumbHeight
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// hashString returns the hex SHA-256 of s.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// peekImage describes a PNG without writing it.
func peekImage(pngData []byte) (Entry, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(pngData))
	if err != nil {
		return Entry{}, err
	}
	sum := sha256.Sum256(pngData)
	return Entry{Kind: store.KindImage, FileName: "image.png", Size: int64(len(pngData)), Width: cfg.Width,
		Height: cfg.Height, SHA256: hex.EncodeToString(sum[:])}, nil
}
