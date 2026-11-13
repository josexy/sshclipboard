package view

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

// HumanSize formats a byte count in binary units, as Windows Explorer
// does: 4.2 MB.
func HumanSize(n int64) string {
	f := float64(n)
	switch {
	case n < 1024:
		return locales.SizeBytes(strconv.FormatInt(n, 10))
	case n < 1024*1024:
		return locales.SizeKB(oneDecimal(f / 1024))
	case n < 1024*1024*1024:
		return locales.SizeMB(oneDecimal(f / (1024 * 1024)))
	}
	return locales.SizeGB(oneDecimal(f / (1024 * 1024 * 1024)))
}

func oneDecimal(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// StatusLabel names a history status.
func StatusLabel(s store.Status) string {
	switch s {
	case store.StatusQueued:
		return locales.StatusQueued()
	case store.StatusUploading:
		return locales.StatusUploading()
	case store.StatusUploaded:
		return locales.StatusUploaded()
	case store.StatusSkipped:
		return locales.StatusSkipped()
	case store.StatusFailed:
		return locales.StatusFailed()
	case store.StatusCancelled:
		return locales.StatusCancelled()
	}
	return string(s)
}

// StatusColor colors a history status.
func StatusColor(t *ui.Theme, s store.Status) ui.Color {
	switch s {
	case store.StatusUploaded:
		return t.Success
	case store.StatusFailed:
		return t.Danger
	case store.StatusUploading, store.StatusQueued:
		return t.Accent
	}
	return t.TextMuted
}

// ReasonText translates a history error: a reason code, or the text of a
// system or server error as it is.
func ReasonText(code string) string {
	switch code {
	case store.ReasonNoMatchingRule:
		return locales.ReasonNoMatchingRule()
	case store.ReasonFolder:
		return locales.ReasonFolder()
	case store.ReasonTooLarge:
		return locales.ReasonTooLarge()
	case store.ReasonExists:
		return locales.ReasonExists()
	case store.ReasonInterrupted:
		return locales.ReasonInterrupted()
	case store.ReasonSourceNotFound:
		return locales.ReasonSourceNotFound()
	case store.ReasonRemoteDirMissing:
		return locales.ReasonRemoteDirMissing()
	case store.ReasonHostKeyChanged:
		return locales.ReasonHostKeyChanged()
	case store.ReasonNoProfile:
		return locales.ReasonNoProfile()
	case store.ReasonCancelled:
		return locales.ReasonCancelled()
	}
	return code
}

// RuleText names the deciding rule of an entry.
func RuleText(name string) string {
	switch name {
	case store.RuleDefault:
		return locales.RuleDefault()
	case store.RuleManual:
		return locales.RuleManual()
	}
	return name
}

// TimeText shows a capture time: the time for today, else the date too.
func TimeText(at, now time.Time) string {
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return locales.TimeToday(at)
	}
	return locales.TimeEarlier(at)
}

// StepText names a connection test step.
func StepText(s sshconn.Step) string {
	switch s {
	case sshconn.StepConnect:
		return locales.StepConnect()
	case sshconn.StepProxy:
		return locales.StepProxy()
	case sshconn.StepHostKey:
		return locales.StepHostKey()
	case sshconn.StepAuth:
		return locales.StepAuth()
	case sshconn.StepRemoteDir:
		return locales.StepRemoteDir()
	}
	return string(s)
}

// ErrorText translates a validation error for a form field.
func ErrorText(err error) string {
	var pe *naming.ParseError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, core.ErrRequired), errors.Is(err, rules.ErrRequired):
		return locales.ErrRequired()
	case errors.Is(err, core.ErrPortRange):
		return locales.ErrPortRange()
	case errors.Is(err, core.ErrNameTaken):
		return locales.ErrNameTaken()
	case errors.Is(err, core.ErrFileMode):
		return locales.ErrFileMode()
	case errors.Is(err, rules.ErrMinOverMax):
		return locales.ErrMinOverMax()
	case errors.Is(err, rules.ErrNegative):
		return locales.ErrNegative()
	case errors.Is(err, core.ErrHotkeyTaken):
		return locales.HotkeyTaken()
	case errors.As(err, &pe):
		switch pe.Kind {
		case naming.ErrUnknownToken:
			return locales.ErrUnknownToken(pe.Token, pe.Pos)
		case naming.ErrUnbalanced:
			return locales.ErrUnbalanced(pe.Pos)
		case naming.ErrBadArgument:
			return locales.ErrBadArgument(pe.Token, pe.Pos)
		case naming.ErrNoArgument:
			return locales.ErrNoArgument(pe.Token)
		case naming.ErrNotAllowed:
			return locales.ErrNotAllowed(pe.Token)
		case naming.ErrTraversal:
			return locales.ErrTraversal()
		case naming.ErrEmptyTemplate:
			return locales.ErrEmptyTemplate()
		}
	}
	return locales.ErrInvalid(err.Error())
}

// ConnLabel names a connection state.
func ConnLabel(s sshconn.Status) string {
	switch s {
	case sshconn.StatusConnected:
		return locales.ConnConnected()
	case sshconn.StatusConnecting:
		return locales.ConnConnecting()
	case sshconn.StatusError:
		return locales.ConnError()
	}
	return locales.ConnDisconnected()
}

// ConnColor colors a connection state's dot.
func ConnColor(t *ui.Theme, s sshconn.Status) ui.Color {
	switch s {
	case sshconn.StatusConnected:
		return t.Success
	case sshconn.StatusConnecting:
		return t.Warning
	case sshconn.StatusError:
		return t.Danger
	}
	return t.TextMuted
}

// parseOptionalInt reads an optional number field: "" is nil.
func parseOptionalInt(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, errors.New(locales.ErrNumber())
	}
	return &n, nil
}

// formatOptional writes an optional number for a field.
func formatOptional[T int | int64](p *T) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}
