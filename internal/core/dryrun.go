package core

import (
	"path"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
)

// DryRunResult is what one clipboard entry would do.
type DryRunResult struct {
	Name     string
	Kind     store.Kind
	RuleName string // a rule's name, store.RuleDefault, or "" for a part skipped by capture
	Action   store.Action
	// RemotePath is where it would go, with "~" resolved when the server
	// is connected, else as the template gives it.
	RemotePath string
	Reason     string // why it would be skipped, a store.Reason* code or an error
}

// DryRun reports, for each entry on the clipboard, the rule that would
// match, the action, and the remote path, without uploading or recording
// anything: the same rules and naming as real captures.
func (c *Controller) DryRun(done func([]DryRunResult)) {
	go func() {
		it, ok := c.cap.Peek()
		c.post(func() {
			var out []DryRunResult
			if ok {
				out = c.dryRun(it)
			}
			if done != nil {
				c.o.Notify(func() { done(out) })
			}
		})
	}()
}

func (c *Controller) dryRun(it capture.Item) []DryRunResult {
	prof := c.activeProfile()
	var k *counter
	if prof != nil {
		// Peek at the counter: the run must not advance it.
		k = &counter{c: c, profileID: prof.ID, day: store.Day(it.CapturedAt)}
	}
	var out []DryRunResult
	for _, e := range it.Entries {
		d := c.l.ruleSet.Evaluate(rules.Entry{Kind: e.Kind, FileName: e.FileName, Size: e.Size,
			Width: e.Width, Height: e.Height, Text: e.Text})
		r := DryRunResult{Name: e.FileName, Kind: e.Kind, RuleName: d.RuleName, Action: d.Action}
		switch {
		case d.Action == store.ActionSkip:
			if d.Default() {
				r.Reason = store.ReasonNoMatchingRule
			}
		case prof == nil:
			r.Reason = store.ReasonNoProfile
		default:
			dir, name, err := c.remoteName(*prof, e.Kind, e.FileName, e.LocalPath, e.SHA256, it.CapturedAt, d, k)
			if err != nil {
				r.Reason = err.Error()
				break
			}
			r.RemotePath = path.Join(dir, name)
			if ev, ok := c.l.conn[prof.ID]; ok && ev.Status == sshconn.StatusConnected {
				if conn, err := c.pool.Get(ctx, sshconn.TargetOf(*prof)); err == nil {
					r.RemotePath = conn.Resolve(r.RemotePath)
				}
			}
		}
		out = append(out, r)
	}
	for _, sk := range it.Skipped {
		out = append(out, DryRunResult{Name: sk.FileName, Kind: sk.Kind, Action: store.ActionSkip, Reason: sk.Reason})
	}
	return out
}
