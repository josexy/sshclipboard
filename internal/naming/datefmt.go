package naming

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// datePart is a field or a literal of a date FORMAT.
type datePart struct {
	field string // one of dateFields, or "" for lit
	lit   string
}

// dateFields, longest first, so YYYY wins over YY and SSS is one field.
var dateFields = []string{"YYYY", "SSS", "YY", "MM", "DD", "HH", "mm", "ss"}

// parseDateFormat reads a FORMAT: the fields YYYY, YY, MM, DD, HH, mm, ss,
// SSS, and literal text, which is anything but a letter. A letter that is
// not part of a field is an error; bad is its 0-based rune offset.
//
// Fields are formatted directly rather than translated to a Go layout, so
// literal digits or words in a FORMAT ("1", "Jan") are never read as Go
// layout elements.
func parseDateFormat(format string) (parts []datePart, bad int, err error) {
	if format == "" {
		return nil, 0, errors.New("FORMAT is empty")
	}
	runes := []rune(format)
	var lit strings.Builder
	for i := 0; i < len(runes); {
		matched := ""
		for _, f := range dateFields {
			if strings.HasPrefix(string(runes[i:]), f) {
				matched = f
				break
			}
		}
		switch {
		case matched != "":
			if lit.Len() > 0 {
				parts = append(parts, datePart{lit: lit.String()})
				lit.Reset()
			}
			parts = append(parts, datePart{field: matched})
			i += len(matched)
		case unicode.IsLetter(runes[i]):
			return nil, i, fmt.Errorf("FORMAT has %q, which is not one of YYYY, YY, MM, DD, HH, mm, ss, SSS", runes[i])
		default:
			lit.WriteRune(runes[i])
			i++
		}
	}
	if lit.Len() > 0 {
		parts = append(parts, datePart{lit: lit.String()})
	}
	return parts, 0, nil
}

func formatDate(parts []datePart, t time.Time) string {
	var b strings.Builder
	for _, p := range parts {
		switch p.field {
		case "":
			b.WriteString(p.lit)
		case "YYYY":
			fmt.Fprintf(&b, "%04d", t.Year())
		case "YY":
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case "MM":
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case "DD":
			fmt.Fprintf(&b, "%02d", t.Day())
		case "HH":
			fmt.Fprintf(&b, "%02d", t.Hour())
		case "mm":
			fmt.Fprintf(&b, "%02d", t.Minute())
		case "ss":
			fmt.Fprintf(&b, "%02d", t.Second())
		case "SSS":
			fmt.Fprintf(&b, "%03d", t.Nanosecond()/int(time.Millisecond))
		}
	}
	return b.String()
}

// FormatDate formats t with a FORMAT, for previews and tests.
func FormatDate(format string, t time.Time) (string, error) {
	parts, _, err := parseDateFormat(format)
	if err != nil {
		return "", err
	}
	return formatDate(parts, t), nil
}

var (
	fmtDate     = mustFormat("YYYY-MM-DD")
	fmtTime     = mustFormat("HHmmss")
	fmtDatetime = mustFormat("YYYYMMDD-HHmmss")
)

func mustFormat(f string) []datePart {
	p, _, err := parseDateFormat(f)
	if err != nil {
		panic(err)
	}
	return p
}
