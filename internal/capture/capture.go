package capture

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/josexy/sshclipboard/internal/clipwatch"
	"github.com/josexy/sshclipboard/internal/store"
)

// Options configure a Capturer. Zero durations take the defaults.
type Options struct {
	Source  Source
	Watcher clipwatch.Watcher
	// StagingDir returns the staging folder; it is read for each capture,
	// so a changed setting applies to the next one.
	StagingDir func() string
	// Mode returns the capture mode; Confidential whether confidential
	// content is protected. Both are read for each change.
	Mode         func() store.CaptureMode
	Confidential func() bool
	// OnItem receives each captured item, on the Capturer's goroutine for
	// automatic captures and on CaptureNow's caller's for manual ones.
	OnItem func(Item)
	// OnError hears captures that failed, such as an unwritable staging
	// folder. It may be nil.
	OnError func(error)

	Debounce     time.Duration // quiet time that ends a burst; default 300 ms
	DedupeWindow time.Duration // default 5 s
	SelfWindow   time.Duration // how long an own text write is recognized; default 2 s
	Now          func() time.Time
}

// Capturer turns clipboard changes into items.
type Capturer struct {
	opts  Options
	stage stager

	mu        sync.Mutex
	paused    bool
	lastHash  string // kind + content hash of the last captured item
	lastAt    time.Time
	selfText  string // hash of the text the app last wrote
	selfAt    time.Time
	stop      chan struct{}
	done      chan struct{}
	pauseGen  int // bumped by Pause, so a pending debounce is dropped
	captureMu sync.Mutex
}

// New returns a Capturer; Run starts it.
func New(opts Options) *Capturer {
	if opts.Debounce <= 0 {
		opts.Debounce = 300 * time.Millisecond
	}
	if opts.DedupeWindow <= 0 {
		opts.DedupeWindow = 5 * time.Second
	}
	if opts.SelfWindow <= 0 {
		opts.SelfWindow = 2 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Mode == nil {
		opts.Mode = func() store.CaptureMode { return store.CaptureAuto }
	}
	if opts.Confidential == nil {
		opts.Confidential = func() bool { return true }
	}
	if opts.OnError == nil {
		opts.OnError = func(error) {}
	}
	return &Capturer{opts: opts, stage: stager{dir: opts.StagingDir}, stop: make(chan struct{}), done: make(chan struct{})}
}

// Run watches for changes until Close. In automatic mode, a burst of
// changes settles for Debounce and is then captured once. Changes the app
// made itself, changes while paused, and changes in manual mode are
// ignored.
func (c *Capturer) Run() {
	defer close(c.done)
	var (
		timer   *time.Timer
		timerC  <-chan time.Time
		gen     int
		changes = c.opts.Watcher.Changes()
	)
	for {
		select {
		case <-c.stop:
			if timer != nil {
				timer.Stop()
			}
			return
		case ch, ok := <-changes:
			if !ok {
				return
			}
			c.mu.Lock()
			paused, g := c.paused, c.pauseGen
			c.mu.Unlock()
			if ch.Self || paused || c.opts.Mode() != store.CaptureAuto {
				continue
			}
			gen = g
			if timer == nil {
				timer = time.NewTimer(c.opts.Debounce)
			} else {
				timer.Reset(c.opts.Debounce)
			}
			timerC = timer.C
		case <-timerC:
			timerC = nil
			c.mu.Lock()
			stale := c.paused || c.pauseGen != gen
			c.mu.Unlock()
			if stale || c.opts.Mode() != store.CaptureAuto {
				continue
			}
			// The change that ended the burst may have been our own.
			if c.opts.Watcher.OwnedBySelf() {
				continue
			}
			if item, ok := c.capture(store.TriggerAuto, false); ok {
				c.opts.OnItem(item)
			}
		}
	}
}

// Close stops Run.
func (c *Capturer) Close() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	<-c.done
}

// SetPaused pauses or resumes automatic capture. Content copied while
// paused is not captured on resume; only the next change is.
func (c *Capturer) SetPaused(paused bool) {
	c.mu.Lock()
	c.paused = paused
	c.pauseGen++
	c.mu.Unlock()
}

// Paused tells whether automatic capture is paused.
func (c *Capturer) Paused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// NoteSelfWrite records text the app is about to put on the clipboard, so
// that the change is not captured where the watcher can't tell the owner.
func (c *Capturer) NoteSelfWrite(text string) {
	c.mu.Lock()
	c.selfText, c.selfAt = hashString(text), c.opts.Now()
	c.mu.Unlock()
}

// CaptureNow captures the clipboard as "Upload clipboard now" does, in
// either mode and while paused, without duplicate suppression; rules are
// bypassed downstream because the trigger is manual. ok is false when the
// clipboard holds nothing to capture.
func (c *Capturer) CaptureNow() (item Item, ok bool) {
	item, ok = c.capture(store.TriggerManual, false)
	if ok {
		c.opts.OnItem(item)
	}
	return item, ok
}

// Peek reads and classifies the clipboard as a capture would, without
// staging, recording, or suppressing anything: for "Test with current
// clipboard". Entries have no local path for images and text.
func (c *Capturer) Peek() (Item, bool) {
	return c.capture(store.TriggerAuto, true)
}

// capture reads, classifies, filters, and stages the clipboard. dry
// classifies only: nothing is written and nothing suppressed.
func (c *Capturer) capture(trigger store.Trigger, dry bool) (Item, bool) {
	c.captureMu.Lock()
	defer c.captureMu.Unlock()
	now := c.opts.Now()
	seq := c.opts.Watcher.Seq()
	src := c.opts.Source
	formats := src.Formats()
	if c.opts.Confidential() && confidential(src, formats) {
		return Item{}, false // nothing is captured or recorded
	}
	item := Item{CaptureID: NewID(now), Trigger: trigger, CapturedAt: now, Seq: seq}

	has := func(f string) bool { return slices.Contains(formats, f) }
	// Files, then image, then text: one kind per change.
	if has(FormatFileList) || has(FormatURIList) {
		if files, err := src.Files(); err == nil && len(files) > 0 {
			c.files(&item, files)
		}
	}
	if item.Kind == "" && has(FormatPNG) {
		data, err := src.Image()
		switch {
		case errors.Is(err, ErrTooLarge) || len(data) > MaxBytes:
			item.Kind = store.KindImage
			item.Skipped = []Skipped{{Kind: store.KindImage, FileName: "image.png", Reason: store.ReasonTooLarge}}
			item.hash = "too-large"
		case err == nil && len(data) > 0 && dry:
			e, err := peekImage(data)
			if err != nil {
				return Item{}, false
			}
			item.Kind, item.Entries, item.hash = store.KindImage, []Entry{e}, e.SHA256
		case err == nil && len(data) > 0:
			e, err := c.stage.image(item.CaptureID, now, data)
			if err != nil {
				c.opts.OnError(fmt.Errorf("staging the image: %w", err))
				return Item{}, false
			}
			item.Kind, item.Entries, item.hash = store.KindImage, []Entry{e}, e.SHA256
		}
	}
	if item.Kind == "" {
		text, err := src.Text()
		switch {
		case errors.Is(err, ErrTooLarge) || len(text) > MaxBytes:
			item.Kind = store.KindText
			item.Skipped = []Skipped{{Kind: store.KindText, FileName: "text.txt", Reason: store.ReasonTooLarge}}
			item.hash = "too-large"
		case err != nil || strings.TrimSpace(text) == "":
			return Item{}, false // nothing to capture
		default:
			hash := hashString(text)
			c.mu.Lock()
			self := c.selfText == hash && now.Sub(c.selfAt) < c.opts.SelfWindow
			c.mu.Unlock()
			if self && trigger == store.TriggerAuto && !dry {
				return Item{}, false // the app's own write
			}
			if dry {
				item.Kind, item.hash = store.KindText, hash
				item.Entries = []Entry{{Kind: store.KindText, FileName: "text.txt", Size: int64(len(text)), SHA256: hash, Text: text}}
				break
			}
			e, err := c.stage.text(item.CaptureID, now, text)
			if err != nil {
				c.opts.OnError(fmt.Errorf("staging the text: %w", err))
				return Item{}, false
			}
			item.Kind, item.Entries, item.hash = store.KindText, []Entry{e}, e.SHA256
		}
	}

	key := string(item.Kind) + ":" + item.hash
	c.mu.Lock()
	dup := trigger == store.TriggerAuto && key == c.lastHash && now.Sub(c.lastAt) < c.opts.DedupeWindow
	if !dup {
		c.lastHash, c.lastAt = key, now
	}
	c.mu.Unlock()
	if dup {
		for _, e := range item.Entries {
			if e.Staged {
				os.Remove(e.LocalPath)
				if e.ThumbPath != "" {
					os.Remove(e.ThumbPath)
				}
			}
		}
		return Item{}, false
	}
	return item, true
}

// files fills a files item: folders are left out and recorded as
// skipped; files are referred to where they are, not copied or read.
func (c *Capturer) files(item *Item, paths []string) {
	item.Kind = store.KindFile
	var key []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			// Gone already: let the upload report "source file not found".
			item.Entries = append(item.Entries, Entry{Kind: store.KindFile, FileName: filepath.Base(p), LocalPath: p})
			key = append(key, p)
			continue
		}
		if fi.IsDir() {
			item.Skipped = append(item.Skipped, Skipped{Kind: store.KindFile, FileName: filepath.Base(p), LocalPath: p,
				Reason: store.ReasonFolder})
			continue
		}
		item.Entries = append(item.Entries, Entry{Kind: store.KindFile, FileName: filepath.Base(p), LocalPath: p,
			Size: fi.Size()})
		key = append(key, fmt.Sprintf("%s|%d|%d", p, fi.Size(), fi.ModTime().UnixNano()))
	}
	sort.Strings(key)
	item.hash = hashString(strings.Join(key, "\n"))
}

// confidential tells whether the clipboard's source marked it confidential
// or excluded from clipboard history and monitoring, as password managers
// (KeePass, 1Password, Bitwarden) do on Windows.
func confidential(src Source, formats []string) bool {
	if slices.Contains(formats, FormatExcludeFromMonitor) || slices.Contains(formats, FormatViewerIgnore) {
		return true
	}
	if slices.Contains(formats, FormatCanIncludeHistory) {
		// A DWORD; 0 excludes the content from clipboard history.
		b, err := src.Format(FormatCanIncludeHistory)
		if err == nil && len(b) >= 4 && binary.LittleEndian.Uint32(b) == 0 {
			return true
		}
	}
	return false
}
