package core

import "sync"

// Registrar registers system-wide hotkeys. mygo.GlobalShortcut implements
// it.
type Registrar interface {
	Register(accelerator string, fn func()) error
	Unregister(accelerator string)
}

// Hotkey keeps the "Upload clipboard now" hotkey registered.
type Hotkey struct {
	reg Registrar
	fn  func()

	mu      sync.Mutex
	current string
}

// NewHotkey returns a Hotkey that runs fn when pressed.
func NewHotkey(reg Registrar, fn func()) *Hotkey { return &Hotkey{reg: reg, fn: fn} }

// Current returns the registered accelerator, "" for none.
func (h *Hotkey) Current() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

// Start registers the saved accelerator at launch. A failure leaves none
// registered.
func (h *Hotkey) Start(acc string) error { return h.Swap("", acc) }

// Swap replaces the hotkey: it registers next first and only then
// unregisters prev, so a hotkey another application holds fails and the
// previous one stays active. next "" disables it. It suits Hooks.Hotkey.
func (h *Hotkey) Swap(prev, next string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if prev == "" {
		prev = h.current
	}
	if next == prev {
		return nil
	}
	if next != "" {
		if err := h.reg.Register(next, h.fn); err != nil {
			return err
		}
	}
	if prev != "" {
		h.reg.Unregister(prev)
	}
	h.current = next
	return nil
}
