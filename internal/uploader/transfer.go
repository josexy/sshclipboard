package uploader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
)

// Job is one entry to upload.
type Job struct {
	HistoryID int64
	CaptureID string
	Target    sshconn.Target
	// Source is the local file: a staged image or text, or the user's
	// original file, read when the transfer starts.
	Source     string
	Dir        string // rendered directory: "~/x", "x" (under home), or "/x"
	Name       string // final file name, already sanitized
	Policy     store.ConflictPolicy
	CreateDirs bool
	FileMode   int // octal mode to apply, or -1
}

// SourceError is a problem with the local file: permanent.
type SourceError struct{ Err error }

func (e *SourceError) Error() string { return e.Err.Error() }
func (e *SourceError) Unwrap() error { return e.Err }

// ErrSourceNotFound is the error of an entry whose local file is gone.
var ErrSourceNotFound = errors.New("source file not found")

// ErrExists marks an entry skipped by the skip conflict policy.
var ErrExists = errors.New("already exists")

// ErrNoFreeName is returned when name-1 … name-999 are all taken.
var ErrNoFreeName = errors.New("no free name: -1 to -999 are taken")

// source opens a local file for upload.
type source func(path string) (io.ReadCloser, int64, error)

func openFile(p string) (io.ReadCloser, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, &SourceError{ErrSourceNotFound}
		}
		return nil, 0, &SourceError{err}
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, &SourceError{err}
	}
	if fi.IsDir() {
		f.Close()
		return nil, 0, &SourceError{errors.New("the source is a folder")}
	}
	return f, fi.Size(), nil
}

// progressReader counts bytes read and reports progress at most once per
// interval. It fails with the context's error once ctx is done, which
// stops a transfer within one chunk.
type progressReader struct {
	ctx      context.Context
	r        io.Reader
	total    int64
	sent     int64
	every    time.Duration
	last     time.Time
	lastSent int64
	report   func(sent, total int64, speed float64)
	now      func() time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	if err != nil && err != io.EOF {
		err = &SourceError{err} // the local file failed, not the server
	}
	p.sent += int64(n)
	if now := p.now(); now.Sub(p.last) >= p.every {
		elapsed := now.Sub(p.last).Seconds()
		speed := 0.0
		if !p.last.IsZero() && elapsed > 0 {
			speed = float64(p.sent-p.lastSent) / elapsed
		}
		p.last, p.lastSent = now, p.sent
		p.report(p.sent, p.total, speed)
	}
	return n, err
}

// partName returns the temporary name of an upload: hidden, unique, and
// next to the final name so the rename stays on one filesystem.
func partName(name string) string {
	return "." + name + "." + naming.RandString(nil, 6) + ".part"
}

// numbered returns name with -n before its extension: report-2.pdf.
func numbered(name string, n int) string {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" { // a dotfile such as ".env"
		base, ext = name, ""
	}
	return fmt.Sprintf("%s-%d%s", base, n, ext)
}

// attemptState carries what one attempt left behind for the next.
type attemptState struct {
	stalePart string // a .part a lost connection kept us from removing
}

// transfer uploads j once over conn: it resolves and prepares the
// directory, writes a temporary file, applies the mode, and commits it by
// the conflict policy. It returns the final absolute path.
func (u *Uploader) transfer(ctx context.Context, conn *sshconn.Conn, j Job, st *attemptState,
	report func(sent, total int64, speed float64)) (string, error) {
	c := conn.SFTP
	if st.stalePart != "" {
		// The connection allows it now.
		if err := c.Remove(st.stalePart); err == nil || errors.Is(err, fs.ErrNotExist) {
			st.stalePart = ""
		}
	}

	dir := conn.Resolve(j.Dir)
	if j.CreateDirs {
		if err := c.MkdirAll(dir); err != nil {
			return "", fmt.Errorf("creating %s: %w", dir, err)
		}
	} else if fi, err := c.Stat(dir); err != nil || !fi.IsDir() {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return "", fmt.Errorf("%s: %w", dir, sshconn.ErrRemoteDirMissing)
	}
	final := path.Join(dir, j.Name)
	if j.Policy == store.ConflictSkip {
		if _, err := c.Stat(final); err == nil {
			return final, ErrExists
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}

	src, size, err := u.open(j.Source)
	if err != nil {
		return "", err
	}
	defer src.Close()

	part := path.Join(dir, partName(j.Name))
	f, err := c.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_TRUNC)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", part, err)
	}
	committed := false
	defer func() {
		if !committed {
			f.Close()
			// Remove the temporary file when the connection allows;
			// otherwise the next attempt does.
			if rerr := c.Remove(part); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				st.stalePart = part
			}
		}
	}()

	pr := &progressReader{ctx: ctx, r: src, total: size, every: u.opts.ProgressInterval, report: report, now: time.Now}
	report(0, size, 0)
	if _, err := f.ReadFrom(pr); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	report(pr.sent, size, 0)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if j.FileMode >= 0 {
		if err := c.Chmod(part, os.FileMode(j.FileMode)); err != nil {
			return "", fmt.Errorf("setting mode %04o: %w", j.FileMode, err)
		}
	}
	got, err := commit(c, part, dir, j.Name, j.Policy)
	if err != nil {
		return "", err
	}
	committed = true
	return got, nil
}

// renamer is the part of *sftp.Client that commit uses.
type renamer interface {
	Rename(oldname, newname string) error
	PosixRename(oldname, newname string) error
	Remove(path string) error
	Stat(path string) (fs.FileInfo, error)
	HasExtension(name string) (string, bool)
}

// commit gives the temporary file its final name by the policy and
// returns the final path.
func commit(c renamer, part, dir, name string, policy store.ConflictPolicy) (string, error) {
	switch policy {
	case store.ConflictOverwrite:
		final := path.Join(dir, name)
		if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
			if err := c.PosixRename(part, final); err != nil {
				return "", err
			}
			return final, nil
		}
		if err := c.Remove(final); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if err := c.Rename(part, final); err != nil {
			return "", err
		}
		return final, nil
	default: // rename, and skip (whose existing target was caught earlier)
		for n := 0; n <= 999; n++ {
			candidate := name
			if n > 0 {
				candidate = numbered(name, n)
			}
			final := path.Join(dir, candidate)
			// Some servers' rename overwrites; never rename onto a name
			// that is visibly taken.
			if _, err := c.Stat(final); err == nil {
				if policy == store.ConflictSkip {
					return final, ErrExists
				}
				continue
			}
			err := c.Rename(part, final)
			if err == nil {
				return final, nil
			}
			// OpenSSH's rename fails when the target appeared meanwhile.
			if _, serr := c.Stat(final); serr == nil {
				if policy == store.ConflictSkip {
					return final, ErrExists
				}
				continue
			}
			return "", err
		}
		return "", ErrNoFreeName
	}
}
