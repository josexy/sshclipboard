//go:build !windows

package clipwatch

// New returns the portable fallback watcher, which polls fingerprint once
// a second.
func New(fingerprint func() string) Watcher { return NewFallback(fingerprint, 0) }
