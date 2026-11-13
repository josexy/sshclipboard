package core

import "github.com/josexy/sshclipboard/internal/store"

// NoticeKind is what a notification is about.
type NoticeKind int

const (
	NoticeUploaded     NoticeKind = iota + 1 // an entry uploaded (RemotePath)
	NoticeFailed                             // an entry failed (Err)
	NoticeHostKey                            // a server needs its host key verified
	NoticeStillRunning                       // the window closed to the tray
)

// Notice is a notification for the Notifier to show. The app formats its
// text from the message catalog.
type Notice struct {
	Kind        NoticeKind
	HistoryID   int64
	Name        string
	RemotePath  string
	ProfileName string
	Err         error
}

// ShouldNotify applies the notification setting to upload outcomes: All
// notifies successes and failures, Errors only failures, Off nothing.
// Other notices (host key verification, still running) are always shown.
func ShouldNotify(policy store.NotifyPolicy, kind NoticeKind) bool {
	switch kind {
	case NoticeUploaded:
		return policy == store.NotifyAll
	case NoticeFailed:
		return policy == store.NotifyAll || policy == store.NotifyErrors
	}
	return true
}

// TrayState is the tray icon's state.
type TrayState string

const (
	TrayMonitoring TrayState = "monitoring"
	TrayPaused     TrayState = "paused"
	TrayUploading  TrayState = "uploading"
	TrayError      TrayState = "error"
)

// DeriveTray picks the tray icon: an error (the last upload failed, or a
// server waits for verification) wins until cleared, then a running
// upload, then paused, else monitoring.
func DeriveTray(monitoring, uploading, errorBadge bool) TrayState {
	switch {
	case errorBadge:
		return TrayError
	case uploading:
		return TrayUploading
	case !monitoring:
		return TrayPaused
	}
	return TrayMonitoring
}
