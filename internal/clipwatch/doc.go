// Package clipwatch detects clipboard changes. On Windows it polls
// GetClipboardSequenceNumber and reports the clipboard owner's process; on
// other systems a portable fallback polls the clipboard contents.
package clipwatch
