package core

import (
	"errors"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/uploader"
)

// onUpload records an upload event in history and the state.
func (c *Controller) onUpload(ev uploader.Event) {
	id := ev.Job.HistoryID
	now := c.o.Now()
	set := func(u store.Update) {
		if err := c.st.History.SetStatus(ctx, id, u); err != nil && !errors.Is(err, store.ErrNotFound) {
			c.fail(err)
		}
	}
	switch ev.Kind {
	case uploader.Started:
		c.l.active[id] = store.StatusUploading
		set(store.Update{Status: store.StatusUploading, Attempts: ev.Attempt})
	case uploader.Progress:
		c.l.progress[id] = Progress{Sent: ev.Sent, Total: ev.Total, Speed: ev.Speed}
		return // no history change
	case uploader.Parked:
		// Waits for the user to trust the key; nothing was sent.
		c.l.active[id] = store.StatusQueued
		set(store.Update{Status: store.StatusQueued})
		c.askHostKey(ev.Job.Target.ProfileID, ev.Err)
	case uploader.Done:
		set(store.Update{Status: store.StatusUploaded, Attempts: ev.Attempt, RemotePath: ev.RemotePath, FinishedAt: now})
		c.l.errorBadge = false
		c.notify(Notice{Kind: NoticeUploaded, HistoryID: id, Name: ev.Job.Name, RemotePath: ev.RemotePath})
		c.resolved(id, ev.RemotePath)
	case uploader.Skipped:
		set(store.Update{Status: store.StatusSkipped, Attempts: ev.Attempt, Error: store.ReasonExists, FinishedAt: now})
		c.resolved(id, "")
	case uploader.Cancelled:
		set(store.Update{Status: store.StatusCancelled, Attempts: ev.Attempt, FinishedAt: now})
		c.resolved(id, "")
	case uploader.Failed:
		set(store.Update{Status: store.StatusFailed, Attempts: ev.Attempt, Error: reason(ev.Err), FinishedAt: now})
		c.l.errorBadge = true
		c.notify(Notice{Kind: NoticeFailed, HistoryID: id, Name: ev.Job.Name, Err: ev.Err})
		var hk *sshconn.HostKeyError
		var need *sshconn.SecretNeededError
		switch {
		case errors.As(ev.Err, &hk):
			c.askHostKey(ev.Job.Target.ProfileID, hk)
		case errors.As(ev.Err, &need):
			c.askSecret(ev.Job.Target.ProfileID, need.Kind)
		}
		c.resolved(id, "")
	}
	if ev.Kind != uploader.Started && ev.Kind != uploader.Parked {
		delete(c.l.active, id)
		delete(c.l.progress, id)
		// Close connections of inactive profiles with nothing to do.
		active := c.l.settings.ActiveProfile
		c.pool.CloseIdle(func(pid int64) bool { return pid == active || c.up.Busy(pid) })
	}
	c.refreshHistory()
}

// reason returns the history error of a failure: a reason code the view
// translates, or the error's text.
func reason(err error) string {
	var hk *sshconn.HostKeyError
	switch {
	case errors.Is(err, uploader.ErrSourceNotFound):
		return store.ReasonSourceNotFound
	case errors.Is(err, sshconn.ErrRemoteDirMissing):
		return store.ReasonRemoteDirMissing
	case errors.As(err, &hk) && hk.Changed():
		return store.ReasonHostKeyChanged
	}
	return err.Error()
}

// resolved notes that an entry finished, and copies the remote paths of
// its capture back to the clipboard once all its entries did.
func (c *Controller) resolved(id int64, remotePath string) {
	pc := c.l.captures[id]
	if pc == nil {
		return
	}
	delete(c.l.captures, id)
	delete(pc.waiting, id)
	if remotePath != "" {
		pc.paths[id] = remotePath
	}
	if len(pc.waiting) > 0 {
		return
	}
	var paths []string
	for _, hid := range pc.order {
		if p, ok := pc.paths[hid]; ok {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 || !pc.profile.CopyBack {
		return
	}
	// Don't replace content the user copied after the capture.
	if w := c.o.Watcher; w != nil && w.Seq() != pc.seq && !w.OwnedBySelf() {
		return
	}
	c.writeClipboard(CopyBackText(pc.profile, paths))
}

// writeClipboard puts the app's own text on the clipboard, which is not
// captured.
func (c *Controller) writeClipboard(text string) {
	if c.o.Clipboard == nil {
		return
	}
	c.cap.NoteSelfWrite(text)
	c.o.Clipboard.WriteText(text)
}

// onStatus records a connection state change.
func (c *Controller) onStatus(ev sshconn.StatusEvent) {
	c.l.conn[ev.ProfileID] = ev
}

// askHostKey shows the trust (or replace) prompt for a profile, and a
// notification when the window is hidden.
func (c *Controller) askHostKey(profileID int64, err error) {
	var hk *sshconn.HostKeyError
	if !errors.As(err, &hk) {
		return
	}
	name := ""
	if p := c.profile(profileID); p != nil {
		name = p.Name
	}
	c.l.hostKey = &HostKeyPrompt{ProfileID: profileID, ProfileName: name, Err: hk}
	if vis := c.o.Hooks.WindowVisible; vis == nil || !vis() {
		c.notifyAlways(Notice{Kind: NoticeHostKey, ProfileName: name, Err: hk})
	}
}

// askSecret asks for a secret the credential store could not provide.
func (c *Controller) askSecret(profileID int64, kind secret.Kind) {
	name := ""
	if p := c.profile(profileID); p != nil {
		name = p.Name
	}
	c.l.secretAsk = &SecretPrompt{ProfileID: profileID, ProfileName: name, Kind: string(kind)}
}

// notify shows a notification the policy allows.
func (c *Controller) notify(n Notice) {
	if ShouldNotify(c.l.settings.Notifications, n.Kind) {
		c.notifyAlways(n)
	}
}

func (c *Controller) notifyAlways(n Notice) {
	if c.o.Notifier != nil {
		c.o.Notifier.Notify(n)
	}
}
