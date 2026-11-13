package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
)

// ErrHotkeyTaken is returned when another application holds a hotkey.
var ErrHotkeyTaken = errors.New("the hotkey is unavailable: another application uses it")

// ErrNotFound is returned by actions on entries or profiles that are gone.
var ErrNotFound = store.ErrNotFound

// ErrProfileDeleted is returned when retrying an entry whose server was
// deleted.
var ErrProfileDeleted = errors.New("the server of this entry was deleted")

// ErrNoActiveProfile is returned when an action needs an active server.
var ErrNoActiveProfile = errors.New("no server is active")

// --- Settings ---

// SetSetting saves one setting and applies it: monitoring pauses capture,
// appearance changes the theme, start at login registers the app, and
// the hotkey is swapped (failing with ErrHotkeyTaken, keeping the old one).
// A setting that can't be saved keeps its previous value.
func (c *Controller) SetSetting(key string, value any, done func(error)) {
	c.do(func() error {
		prev := c.l.settings
		switch key {
		case store.KeyHotkey:
			next, _ := value.(string)
			if h := c.o.Hooks.Hotkey; h != nil && next != prev.Hotkey {
				if err := h(prev.Hotkey, next); err != nil {
					return fmt.Errorf("%w (%v)", ErrHotkeyTaken, err)
				}
			}
		case store.KeyStartAtLogin:
			on, _ := value.(bool)
			if h := c.o.Hooks.StartAtLogin; h != nil {
				if err := h(on); err != nil {
					return err
				}
			}
		}
		if err := c.st.Settings.Set(ctx, key, value); err != nil {
			// Undo what was applied, so the previous value stays in effect.
			switch key {
			case store.KeyHotkey:
				if h := c.o.Hooks.Hotkey; h != nil {
					h(value.(string), prev.Hotkey)
				}
			case store.KeyStartAtLogin:
				if h := c.o.Hooks.StartAtLogin; h != nil {
					h(prev.StartAtLogin)
				}
			}
			return err
		}
		if err := c.reload(); err != nil {
			return err
		}
		s := c.l.settings
		switch key {
		case store.KeyMonitoring:
			c.cap.SetPaused(!s.Monitoring)
		case store.KeyAppearance:
			if h := c.o.Hooks.Appearance; h != nil {
				h(s.Appearance)
			}
		case store.KeyRetentionCount, store.KeyRetentionDays:
			c.prune()
			c.refreshHistory()
		}
		return nil
	}, done)
}

// SetMonitoring turns automatic capture on or off.
func (c *Controller) SetMonitoring(on bool, done func(error)) {
	c.SetSetting(store.KeyMonitoring, on, done)
}

// CaptureNow captures the clipboard as "Upload clipboard now": in either
// mode, bypassing the rules.
func (c *Controller) CaptureNow() {
	go c.cap.CaptureNow()
}

// --- Profiles ---

// validateTemplates checks a profile's directory and clipboard templates.
func validateTemplates(p store.Profile, fe FieldErrors) {
	if fe[FieldRemoteDir] == nil && strings.TrimSpace(p.RemoteDir) != "" {
		if _, err := naming.ParseDir(p.RemoteDir); err != nil {
			fe[FieldRemoteDir] = err
		}
	}
	if tok := ValidateCopyTemplate(p.CopyTemplate); tok != "" {
		fe[FieldTemplate] = fmt.Errorf("%s is an unknown token", tok)
	}
}

// SaveProfile validates and saves a profile and its secrets. The first
// profile saved while none is active becomes active.
func (c *Controller) SaveProfile(in ProfileInput, done func(SaveResult, error)) {
	c.post(func() {
		res, err := saveProfile(ctx, c.st, c.sec, in, validateTemplates)
		if err == nil && c.l.settings.ActiveProfile == 0 {
			err = c.st.Settings.Set(ctx, store.KeyActiveProfile, res.Profile.ID)
		}
		if err == nil {
			err = c.reload()
			// A changed profile reconnects with its new settings.
			c.pool.Close(res.Profile.ID)
		}
		if err != nil && !isUserError(err) {
			c.fail(err)
		}
		if done != nil {
			c.publish() // done sees the saved profile in the state
			c.o.Notify(func() { done(res, err) })
		}
	})
}

// DeleteProfile deletes a profile with its secrets and trusted host key.
// Deleting the active profile leaves none active, which stops automatic
// uploads.
func (c *Controller) DeleteProfile(id int64, done func(error)) {
	c.do(func() error {
		if err := c.st.Profiles.Delete(ctx, id); err != nil {
			return err
		}
		c.pool.Close(id)
		if hk := c.l.hostKey; hk != nil && hk.ProfileID == id {
			c.l.hostKey = nil
		}
		delete(c.l.conn, id)
		err := c.sec.Delete(id)
		return errors.Join(err, c.reload())
	}, done)
}

// DuplicateProfile copies a profile and its secrets under a new name.
func (c *Controller) DuplicateProfile(id int64, done func(error)) {
	c.do(func() error {
		p := c.profile(id)
		if p == nil {
			return ErrNotFound
		}
		dup := *p
		dup.ID = 0
		taken := map[string]bool{}
		for _, q := range c.l.profiles {
			taken[strings.ToLower(q.Name)] = true
		}
		dup.Name = p.Name + " copy"
		for n := 2; taken[strings.ToLower(dup.Name)]; n++ {
			dup.Name = fmt.Sprintf("%s copy %d", p.Name, n)
		}
		created, err := c.st.Profiles.Create(ctx, dup)
		if err != nil {
			return err
		}
		for _, kind := range secret.Kinds {
			if v, err := c.sec.Get(id, kind); err == nil {
				c.sec.Set(created.ID, kind, v)
			}
		}
		return c.reload()
	}, done)
}

// SetActiveProfile makes a profile the upload target of new captures and
// keeps the choice across restarts. Uploads already queued keep their
// profile.
func (c *Controller) SetActiveProfile(id int64, done func(error)) {
	c.do(func() error {
		if id != 0 && c.profile(id) == nil {
			return ErrNotFound
		}
		if err := c.st.Settings.Set(ctx, store.KeyActiveProfile, id); err != nil {
			return err
		}
		if err := c.reload(); err != nil {
			return err
		}
		c.pool.CloseIdle(func(pid int64) bool { return pid == id || c.up.Busy(pid) })
		return nil
	}, done)
}

// formSecrets reads the secrets a connection test uses: those typed in
// the form, else the stored ones.
type formSecrets struct {
	in     ProfileInput
	stored *secret.Store
}

func (f formSecrets) Get(id int64, kind secret.Kind) (string, error) {
	if in := f.in.secret(kind); in.Set && in.Value != "" {
		return in.Value, nil
	}
	return f.stored.Get(id, kind)
}

// TestConnection tests a profile as the form shows it, with the secrets
// typed in the form or else stored, and reports the result to done. An
// unknown host key opens the trust prompt.
func (c *Controller) TestConnection(in ProfileInput, done func(sshconn.CheckResult)) {
	go func() {
		p := in.Profile
		cfg := *c.sshcfg
		cfg.OnStatus = nil
		cfg.Secrets = formSecrets{in: in, stored: c.sec}
		dir, err := naming.RenderDir(p.RemoteDir, naming.Vars{Now: c.o.Now(), Profile: p.Name, Host: p.Host})
		var res sshconn.CheckResult
		if err != nil {
			res = sshconn.CheckResult{Step: sshconn.StepRemoteDir, Err: err}
		} else {
			res = cfg.Check(context.Background(), sshconn.TargetOf(p), dir, p.CreateDirs)
		}
		var hk *sshconn.HostKeyError
		if errors.As(res.Err, &hk) {
			c.post(func() { c.askHostKey(p.ID, hk) })
		}
		if done != nil {
			c.o.Notify(func() { done(res) })
		}
	}()
}

// --- Host keys and secrets ---

// TrustHostKey trusts (or, for a changed key, replaces) the key of the
// pending prompt and resumes the server's waiting uploads.
func (c *Controller) TrustHostKey(done func(error)) {
	c.do(func() error {
		hk := c.l.hostKey
		if hk == nil {
			return nil
		}
		if err := c.st.KnownHosts.Put(ctx, hk.Err.KnownHost()); err != nil {
			return err
		}
		c.l.hostKey = nil
		c.up.Resume(hk.ProfileID)
		return nil
	}, done)
}

// DismissHostKey closes the prompt; the server's uploads keep waiting.
func (c *Controller) DismissHostKey() {
	c.post(func() { c.l.hostKey = nil })
}

// DismissSecret closes the secret prompt; the entries stay failed.
func (c *Controller) DismissSecret() {
	c.post(func() { c.l.secretAsk = nil })
}

// ProvideSecret keeps a secret the user typed for this session, and
// retries the server's entries that failed for want of it.
func (c *Controller) ProvideSecret(profileID int64, kind secret.Kind, value string, done func(error)) {
	c.do(func() error {
		c.sec.SetSession(profileID, kind, value)
		c.l.secretAsk = nil
		failed, _, err := c.st.History.Query(ctx, store.Filter{Statuses: []store.Status{store.StatusFailed}, Limit: HistoryPageSize})
		if err != nil {
			return err
		}
		// The error names the missing secret (sshconn.SecretNeededError).
		missing := "the " + kind.Noun() + " of "
		for _, e := range failed {
			if e.ProfileID == profileID && strings.Contains(e.Error, missing) {
				c.retry(e)
			}
		}
		return nil
	}, done)
}

// --- Rules and naming ---

// SaveRule validates and saves a rule (a new one, ID 0, goes last).
// Invalid rules fail with *rules.RuleError naming the fields.
func (c *Controller) SaveRule(r store.Rule, done func(error)) {
	c.do(func() error {
		if err := rules.Validate(r); err != nil {
			return err
		}
		var err error
		if r.ID == 0 {
			_, err = c.st.Rules.Create(ctx, r)
		} else {
			err = c.st.Rules.Update(ctx, r)
		}
		return errors.Join(err, c.reload())
	}, done)
}

// DeleteRule deletes a rule.
func (c *Controller) DeleteRule(id int64, done func(error)) {
	c.do(func() error { return errors.Join(c.st.Rules.Delete(ctx, id), c.reload()) }, done)
}

// MoveRule moves a rule up (negative delta) or down.
func (c *Controller) MoveRule(id int64, delta int, done func(error)) {
	c.do(func() error { return errors.Join(c.st.Rules.Move(ctx, id, delta), c.reload()) }, done)
}

// ReorderRules sets the rule order, as a drag and drop does.
func (c *Controller) ReorderRules(ids []int64, done func(error)) {
	c.do(func() error { return errors.Join(c.st.Rules.Reorder(ctx, ids), c.reload()) }, done)
}

// SetRuleEnabled turns a rule on or off.
func (c *Controller) SetRuleEnabled(id int64, on bool, done func(error)) {
	c.do(func() error { return errors.Join(c.st.Rules.SetEnabled(ctx, id, on), c.reload()) }, done)
}

// SetDefaultAction sets what happens when no rule matches.
func (c *Controller) SetDefaultAction(a store.Action, done func(error)) {
	c.SetSetting(store.KeyDefaultAction, a, done)
}

// SaveNaming validates and saves a kind's template and transforms.
func (c *Controller) SaveNaming(n store.Naming, done func(error)) {
	c.do(func() error {
		if _, err := naming.Parse(n.Template); err != nil {
			return FieldErrors{"template": err}
		}
		return errors.Join(c.st.Naming.Save(ctx, n), c.reload())
	}, done)
}

// --- History ---

// SetHistoryFilter changes what the Activity view lists.
func (c *Controller) SetHistoryFilter(f store.Filter) {
	c.post(func() {
		if f.Limit == 0 {
			f.Limit = HistoryPageSize
		}
		c.l.filter = f
		c.refreshHistory()
	})
}

// ActivityViewed clears the tray's error state, as opening Activity does.
func (c *Controller) ActivityViewed() {
	c.post(func() { c.l.errorBadge = false })
}

// DismissNotice forgets the startup notice once shown.
func (c *Controller) DismissNotice() {
	c.post(func() { c.l.notice = ""; c.l.lastError = "" })
}

// retry queues a failed or cancelled entry again, to its original server
// and with its stored name.
func (c *Controller) retry(e store.HistoryEntry) error {
	p := c.profile(e.ProfileID)
	if p == nil {
		return ErrProfileDeleted
	}
	if err := c.st.History.Requeue(ctx, e.ID, store.Requeue{Trigger: e.Trigger, ProfileID: p.ID,
		ProfileName: p.Name, RuleName: e.RuleName, RemoteDir: e.RemoteDir, RemoteName: e.RemoteName}); err != nil {
		return err
	}
	c.track(e.ID, *p)
	c.enqueue(e, *p)
	return nil
}

// track registers a single re-queued entry for copy-back, guarded by the
// clipboard as it is now.
func (c *Controller) track(id int64, p store.Profile) {
	var seq uint32
	if c.o.Watcher != nil {
		seq = c.o.Watcher.Seq()
	}
	c.l.captures[id] = &pendingCapture{seq: seq, profile: p, order: []int64{id},
		waiting: map[int64]bool{id: true}, paths: map[int64]string{}}
}

// Retry queues a failed or cancelled entry again to its original server.
func (c *Controller) Retry(id int64, done func(error)) {
	c.do(func() error {
		e, err := c.st.History.Get(ctx, id)
		if err != nil {
			return err
		}
		if e.Status != store.StatusFailed && e.Status != store.StatusCancelled {
			return fmt.Errorf("only failed or cancelled entries can be retried")
		}
		if err := c.retry(e); err != nil {
			return err
		}
		c.refreshHistory()
		return nil
	}, done)
}

// UploadAnyway uploads a skipped entry as "Upload clipboard now" would:
// to the active server, with its default directory and templates.
func (c *Controller) UploadAnyway(id int64, done func(error)) {
	c.do(func() error {
		e, err := c.st.History.Get(ctx, id)
		if err != nil {
			return err
		}
		if e.Status != store.StatusSkipped || e.LocalPath == "" {
			return fmt.Errorf("this entry can't be uploaded")
		}
		if fi, err := os.Stat(e.LocalPath); err != nil || fi.IsDir() {
			return fmt.Errorf("%s: %w", e.LocalPath, ErrLocalFileGone)
		}
		p := c.activeProfile()
		if p == nil {
			return ErrNoActiveProfile
		}
		k := &counter{c: c, profileID: p.ID, day: store.Day(c.o.Now())}
		d := rules.Decision{RuleName: store.RuleManual, Action: store.ActionUpload}
		dir, name, err := c.remoteName(*p, e.Kind, e.OriginalName, e.LocalPath, e.SHA256, c.o.Now(), d, k)
		if err != nil {
			return err
		}
		if cv := k.value(); cv != nil {
			if _, err := c.st.Counters.Next(ctx, p.ID, cv.Day); err != nil {
				return err
			}
		}
		if err := c.st.History.Requeue(ctx, e.ID, store.Requeue{Trigger: store.TriggerManual, ProfileID: p.ID,
			ProfileName: p.Name, RuleName: store.RuleManual, RemoteDir: dir, RemoteName: name}); err != nil {
			return err
		}
		e.RemoteDir, e.RemoteName, e.Trigger = dir, name, store.TriggerManual
		c.track(e.ID, *p)
		c.enqueue(e, *p)
		c.refreshHistory()
		return nil
	}, done)
}

// ErrLocalFileGone is returned when an entry's local file no longer
// exists.
var ErrLocalFileGone = errors.New("the local file no longer exists")

// CopyRemotePath puts an uploaded entry's remote path on the clipboard;
// the change is not captured.
func (c *Controller) CopyRemotePath(id int64, done func(error)) {
	c.do(func() error {
		e, err := c.st.History.Get(ctx, id)
		if err != nil {
			return err
		}
		if e.RemotePath == "" {
			return fmt.Errorf("this entry has no remote path")
		}
		c.writeClipboard(e.RemotePath)
		return nil
	}, done)
}

// Cancel cancels a queued or uploading entry.
func (c *Controller) Cancel(id int64) {
	c.post(func() { c.up.Cancel(id) })
}

// DeleteEntry deletes an entry and its staged file, cancelling it first
// if it is still uploading. Remote files are never deleted.
func (c *Controller) DeleteEntry(id int64, done func(error)) {
	c.do(func() error {
		c.up.Cancel(id)
		delete(c.l.active, id)
		delete(c.l.captures, id)
		e, err := c.st.History.Delete(ctx, id)
		if err != nil {
			return err
		}
		removeStaged(e)
		c.refreshHistory()
		return nil
	}, done)
}

// ClearHistory deletes every entry that is not queued or uploading, with
// their staged files.
func (c *Controller) ClearHistory(done func(error)) {
	c.do(func() error {
		removed, err := c.st.History.Clear(ctx)
		if err != nil {
			return err
		}
		for _, e := range removed {
			removeStaged(e)
		}
		c.refreshHistory()
		return nil
	}, done)
}

// LocalFileExists tells whether an entry's staged or original file still
// exists, for the Open and Show in Explorer actions.
func LocalFileExists(e store.HistoryEntry) bool {
	if e.LocalPath == "" {
		return false
	}
	_, err := os.Stat(e.LocalPath)
	return err == nil
}

// sweepOrphans deletes staged files no history entry refers to.
func (c *Controller) sweepOrphans() {
	keep, err := c.st.History.StagedFiles(ctx)
	if err != nil {
		c.fail(err)
		return
	}
	root := c.stagingDir()
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !keep[p] {
			os.Remove(p)
		}
		return nil
	})
}
