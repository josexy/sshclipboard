// Package clipwatch detects clipboard changes. On Windows it listens for
// clipboard update messages and falls back to polling the sequence number.
// On other systems, it polls a fingerprint of the clipboard contents.
package clipwatch
