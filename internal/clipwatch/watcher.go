package clipwatch

// Change is one clipboard change.
type Change struct {
	// Seq is the clipboard's sequence number after the change.
	Seq uint32
	// Self is true when this process owns the clipboard after the change,
	// i.e. the app wrote it (copy-back, Copy remote path).
	Self bool
}

// Watcher reports clipboard changes. Changes are delivered on a buffered
// channel; when the reader is slow, older undelivered changes are
// dropped, since only "something changed" matters.
type Watcher interface {
	// Changes delivers each change.
	Changes() <-chan Change
	// Seq returns the clipboard's current sequence number.
	Seq() uint32
	// OwnedBySelf tells whether this process owns the clipboard now.
	OwnedBySelf() bool
	// Close stops watching and closes Changes.
	Close()
}

// send delivers c, replacing an undelivered change if the buffer is full.
func send(ch chan Change, c Change) {
	for {
		select {
		case ch <- c:
			return
		default:
			select {
			case <-ch:
			default:
			}
		}
	}
}
