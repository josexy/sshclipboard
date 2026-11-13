package core

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/uploader"
)

// pendingCapture tracks the uploads of one capture (or one Retry or
// Upload anyway), for copy-back once they are all resolved.
type pendingCapture struct {
	seq     uint32 // the clipboard's sequence number when it was taken
	profile store.Profile
	order   []int64
	waiting map[int64]bool
	paths   map[int64]string // uploaded remote paths
}

// splitName splits "Quarterly Report.pdf" into its base and extension;
// a dotfile has no extension.
func splitName(fileName string) (name, ext string) {
	ext = path.Ext(fileName)
	name = strings.TrimSuffix(fileName, ext)
	if name == "" {
		return fileName, ""
	}
	return name, strings.TrimPrefix(ext, ".")
}

// fileHash returns the hex SHA-256 of a local file, for {hash} in a files
// entry's name.
func fileHash(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// counter hands out {counter} values for one capture: it reads the
// stored value once and counts up; the last value is stored with the
// history rows.
type counter struct {
	c         *Controller
	profileID int64
	day       string
	base      int
	used      int
	loaded    bool
	err       error
}

func (k *counter) next() int {
	if !k.loaded {
		k.base, k.err = k.c.st.Counters.Current(ctx, k.profileID, k.day)
		k.loaded = true
	}
	k.used++
	return k.base + k.used
}

func (k *counter) value() *store.CounterValue {
	if k == nil || k.used == 0 {
		return nil
	}
	return &store.CounterValue{ProfileID: k.profileID, Day: k.day, Value: k.base + k.used}
}

// remoteName renders an entry's directory and file name for profile p:
// the decision's overrides, else the profile's directory and the kind's
// template.
func (c *Controller) remoteName(p store.Profile, kind store.Kind, fileName, localPath, sha string, at time.Time,
	d rules.Decision, k *counter) (dir, name string, err error) {
	base, ext := splitName(fileName)
	vars := naming.Vars{Now: at, Name: base, Ext: ext, Hash: sha, Kind: string(kind), Profile: p.Name, Host: p.Host}
	if k != nil {
		vars.Counter = k.next
	}
	dir, err = rules.ResolveDir(p.RemoteDir, d.DirOverride, vars)
	if err != nil {
		return "", "", err
	}
	n := c.namingOf(kind)
	src := n.Template
	if d.TemplateOverride != "" {
		src = d.TemplateOverride
	}
	t, err := naming.Parse(src)
	if err != nil {
		return "", "", err
	}
	if t.Uses(naming.TokHash) && vars.Hash == "" && localPath != "" {
		vars.Hash = fileHash(localPath)
	}
	name = naming.RenderName(t, vars, naming.Transforms{Lowercase: n.Lowercase, SpacesToUnderscore: n.SpacesToUnderscore})
	if k != nil && k.err != nil {
		return "", "", k.err
	}
	return dir, name, nil
}

func (c *Controller) namingOf(kind store.Kind) store.Naming {
	for _, n := range c.l.naming {
		if n.Kind == kind {
			return n
		}
	}
	for _, n := range store.DefaultNaming() {
		if n.Kind == kind {
			return n
		}
	}
	return store.Naming{Kind: kind, Template: "{name}.{ext}"}
}

// onItem runs a captured item through rules and naming, records every
// entry, and queues the uploads.
func (c *Controller) onItem(it capture.Item) {
	now := c.o.Now()
	prof := c.activeProfile()
	var k *counter
	if prof != nil {
		k = &counter{c: c, profileID: prof.ID, day: store.Day(it.CapturedAt)}
	}
	var rows []store.HistoryEntry
	for _, e := range it.Entries {
		row := store.HistoryEntry{
			CaptureID: it.CaptureID, CapturedAt: it.CapturedAt, Trigger: it.Trigger, Kind: e.Kind,
			OriginalName: e.FileName, LocalPath: e.LocalPath, Staged: e.Staged, ThumbPath: e.ThumbPath,
			Size: e.Size, Width: e.Width, Height: e.Height, SHA256: e.SHA256,
		}
		var d rules.Decision
		if it.Trigger == store.TriggerManual {
			// "Upload clipboard now" bypasses the rules: the profile's
			// directory and the default templates.
			d = rules.Decision{RuleName: store.RuleManual, Action: store.ActionUpload}
		} else {
			d = c.l.ruleSet.Evaluate(rules.Entry{Kind: e.Kind, FileName: e.FileName, Size: e.Size,
				Width: e.Width, Height: e.Height, Text: e.Text})
		}
		row.RuleName, row.Action = d.RuleName, d.Action
		switch {
		case d.Action == store.ActionSkip:
			row.Status, row.FinishedAt = store.StatusSkipped, now
			if d.Default() {
				row.Error = store.ReasonNoMatchingRule
			}
		case prof == nil:
			row.Status, row.Error, row.FinishedAt = store.StatusSkipped, store.ReasonNoProfile, now
		default:
			row.ProfileID, row.ProfileName = prof.ID, prof.Name
			dir, name, err := c.remoteName(*prof, e.Kind, e.FileName, e.LocalPath, e.SHA256, it.CapturedAt, d, k)
			if err != nil {
				row.Status, row.Error, row.FinishedAt = store.StatusFailed, err.Error(), now
			} else {
				row.Status, row.RemoteDir, row.RemoteName = store.StatusQueued, dir, name
			}
		}
		rows = append(rows, row)
	}
	for _, sk := range it.Skipped {
		rows = append(rows, store.HistoryEntry{
			CaptureID: it.CaptureID, CapturedAt: it.CapturedAt, Trigger: it.Trigger, Kind: sk.Kind,
			OriginalName: sk.FileName, LocalPath: sk.LocalPath, Action: store.ActionSkip,
			Status: store.StatusSkipped, Error: sk.Reason, FinishedAt: now,
		})
	}
	if len(rows) == 0 {
		return
	}
	ids, err := c.st.History.Insert(ctx, rows, k.value())
	if err != nil {
		c.fail(err)
		return
	}
	var pc *pendingCapture
	for i, row := range rows {
		if row.Status != store.StatusQueued {
			continue
		}
		row.ID = ids[i]
		if pc == nil {
			pc = &pendingCapture{seq: it.Seq, profile: *prof, waiting: map[int64]bool{}, paths: map[int64]string{}}
		}
		pc.order = append(pc.order, row.ID)
		pc.waiting[row.ID] = true
		c.l.captures[row.ID] = pc
		c.enqueue(row, *prof)
	}
	c.prune()
	c.refreshHistory()
}

// enqueue queues an upload of a history row to profile p.
func (c *Controller) enqueue(row store.HistoryEntry, p store.Profile) {
	c.l.active[row.ID] = store.StatusQueued
	c.up.Enqueue(uploader.Job{
		HistoryID: row.ID, CaptureID: row.CaptureID, Target: sshconn.TargetOf(p), Source: row.LocalPath,
		Dir: row.RemoteDir, Name: row.RemoteName, Policy: p.ConflictPolicy, CreateDirs: p.CreateDirs,
		FileMode: p.FileMode,
	})
}

// prune applies the retention limits and deletes the removed entries'
// staged files.
func (c *Controller) prune() {
	removed, err := c.st.History.Prune(ctx, c.l.settings.RetentionCount, c.l.settings.RetentionDays, c.o.Now())
	if err != nil {
		c.fail(err)
		return
	}
	for _, e := range removed {
		removeStaged(e)
	}
}

// removeStaged deletes an entry's staged file and thumbnail; files
// entries point at the user's own files, which are never deleted.
func removeStaged(e store.HistoryEntry) {
	if !e.Staged {
		return
	}
	for _, p := range []string{e.LocalPath, e.ThumbPath} {
		if p != "" {
			os.Remove(p) // best effort; the orphan sweep retries at startup
		}
	}
}
