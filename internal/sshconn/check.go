package sshconn

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/josexy/sshclipboard/internal/naming"
)

// CheckResult is the outcome of a connection test: the resolved absolute
// remote directory on success, else the first failing step and its error.
type CheckResult struct {
	Dir  string
	Step Step // "" on success
	Err  error
}

// OK tells whether every step passed.
func (r CheckResult) OK() bool { return r.Err == nil }

// Check tests a profile: it connects, authenticates, resolves the remote
// directory (dir is the rendered directory template), creates it when
// createDirs is on or checks that it exists, and writes and removes a
// probe file in it. It uses its own connection, closed before it returns.
func (c *Config) Check(ctx context.Context, t Target, dir string, createDirs bool) CheckResult {
	conn, err := c.Dial(ctx, t)
	if err != nil {
		var de *DialError
		if errors.As(err, &de) {
			return CheckResult{Step: de.Step, Err: de.Err}
		}
		return CheckResult{Step: StepConnect, Err: err}
	}
	defer conn.Close()
	abs := conn.Resolve(dir)
	fail := func(err error) CheckResult { return CheckResult{Dir: abs, Step: StepRemoteDir, Err: err} }
	if createDirs {
		if err := conn.SFTP.MkdirAll(abs); err != nil {
			return fail(fmt.Errorf("creating %s: %w", abs, err))
		}
	} else {
		fi, err := conn.SFTP.Stat(abs)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", abs, ErrRemoteDirMissing))
		}
		if !fi.IsDir() {
			return fail(fmt.Errorf("%s is not a directory", abs))
		}
	}
	probe := path.Join(abs, ".sshclipboard-probe-"+naming.RandString(nil, 8))
	f, err := conn.SFTP.Create(probe)
	if err != nil {
		return fail(fmt.Errorf("writing to %s: %w", abs, err))
	}
	_, werr := f.Write([]byte("sshclipboard"))
	cerr := f.Close()
	rerr := conn.SFTP.Remove(probe)
	if err := errors.Join(werr, cerr, rerr); err != nil {
		return fail(fmt.Errorf("writing to %s: %w", abs, err))
	}
	return CheckResult{Dir: abs}
}

// ErrRemoteDirMissing is the error of an upload or test whose directory
// doesn't exist while "create missing directories" is off.
var ErrRemoteDirMissing = errors.New("remote directory does not exist")
