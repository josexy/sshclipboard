package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"golang.org/x/image/draw"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/locales"
)

//go:embed resources/icon.png
var appIcon []byte

// clipboardSource reads the system clipboard for capture. mygo.Clipboard
// marshals each call to the main thread, so it is safe from capture's
// goroutine.
type clipboardSource struct{}

func (clipboardSource) Formats() []string {
	var out []string
	for _, f := range mygo.Clipboard.Formats() {
		out = append(out, string(f))
	}
	// Registered Windows formats, such as the ones password managers mark
	// content with, keep their names here.
	for _, f := range mygo.Clipboard.AvailableFormats() {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

func (clipboardSource) Files() ([]string, error) { return mygo.Clipboard.ReadFiles() }

func (clipboardSource) Image() ([]byte, error) {
	b, err := mygo.Clipboard.ReadFormat(transfer.PNG)
	return b, tooLarge(err)
}

func (clipboardSource) Text() (string, error) {
	b, err := mygo.Clipboard.ReadFormat(transfer.Text)
	return string(b), tooLarge(err)
}

func (clipboardSource) Format(name string) ([]byte, error) {
	return mygo.Clipboard.ReadFormat(transfer.Format(name))
}

// tooLarge maps MyGo's 64 MiB representation cap to capture.ErrTooLarge.
func tooLarge(err error) error {
	if err != nil && strings.Contains(err.Error(), "exceeds 64 MiB") {
		return fmt.Errorf("%w: %v", capture.ErrTooLarge, err)
	}
	return err
}

// fingerprint identifies the clipboard's content for the portable watcher
// (development builds on other systems): its formats, files, and text.
func fingerprint() string {
	h := sha256.New()
	src := clipboardSource{}
	fmt.Fprint(h, src.Formats())
	files, _ := src.Files()
	fmt.Fprint(h, files)
	text, _ := src.Text()
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// noticeText formats a notification from the message catalog.
func noticeText(n core.Notice) (title, body string) {
	switch n.Kind {
	case core.NoticeUploaded:
		return locales.NotifyUploadedTitle(n.Name), n.RemotePath
	case core.NoticeFailed:
		msg := ""
		if n.Err != nil {
			msg = n.Err.Error()
		}
		return locales.NotifyFailedTitle(n.Name), msg
	case core.NoticeHostKey:
		return locales.NotifyHostKeyTitle(n.ProfileName), locales.NotifyHostKeyBody()
	case core.NoticeStillRunning:
		return locales.NotifyRunningTitle(), locales.NotifyRunningBody()
	}
	return locales.AppName(), ""
}

// notifier shows Windows notifications. Their ID is the history entry's,
// so a click can open Activity.
type notifier struct{}

func (notifier) Notify(n core.Notice) {
	title, body := noticeText(n)
	id := "notice"
	if n.HistoryID != 0 {
		id = strconv.FormatInt(n.HistoryID, 10)
	}
	go mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body}).Show()
}

// TrayIconSize is the size the tray icons are drawn at, for high-DPI
// notification areas.
const TrayIconSize = 32

// trayIcons composes the four tray icons from the app icon: the icon with
// a colored badge in its corner for each state.
func trayIcons(base []byte) (map[core.TrayState][]byte, error) {
	src, err := png.Decode(bytes.NewReader(base))
	if err != nil {
		return nil, err
	}
	badges := map[core.TrayState]color.NRGBA{
		core.TrayMonitoring: {0x3F, 0x7D, 0x4E, 0xFF}, // success green
		core.TrayPaused:     {0x8A, 0x87, 0x7D, 0xFF}, // warm gray
		core.TrayUploading:  {0xC9, 0x64, 0x42, 0xFF}, // terracotta
		core.TrayError:      {0xD6, 0x29, 0x1F, 0xFF}, // bright red, apart from the terracotta
	}
	out := map[core.TrayState][]byte{}
	for state, col := range badges {
		dst := image.NewNRGBA(image.Rect(0, 0, TrayIconSize, TrayIconSize))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		badge(dst, col, state)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, err
		}
		out[state] = buf.Bytes()
	}
	return out, nil
}

// badge draws a state's badge in the icon's bottom-right corner: an
// anti-aliased disc with a light rim and a white glyph, so the state shows
// by shape as well as color.
func badge(dst *image.NRGBA, col color.NRGBA, state core.TrayState) {
	const r, cx, cy = 7.5, TrayIconSize - 8.5, TrayIconSize - 8.5
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	rim := color.NRGBA{0xFA, 0xF9, 0xF5, 0xFF}
	for y := 0; y < TrayIconSize; y++ {
		for x := 0; x < TrayIconSize; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Sqrt(dx*dx + dy*dy)
			blend(dst, x, y, rim, clamp01(r+1.5+0.5-d))
			blend(dst, x, y, col, clamp01(r+0.5-d))
			// The glyph, in the disc's coordinates.
			gx, gy := float64(x)+0.5-cx, float64(y)+0.5-cy
			on := false
			switch state {
			case core.TrayPaused: // two bars
				on = (gx >= -3 && gx <= -1 || gx >= 1 && gx <= 3) && gy >= -3.5 && gy <= 3.5
			case core.TrayUploading: // an up arrow
				on = (gx >= -1 && gx <= 1 && gy >= -1 && gy <= 4) || (gy >= -4.5 && gy <= -0.5 && math.Abs(gx) <= gy+4.5)
			case core.TrayError: // an exclamation mark
				on = gx >= -1 && gx <= 1 && (gy >= -4.5 && gy <= 1 || gy >= 2.5 && gy <= 4.5)
			}
			if on {
				blend(dst, x, y, white, 1)
			}
		}
	}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// blend draws col over the pixel at alpha.
func blend(dst *image.NRGBA, x, y int, col color.NRGBA, alpha float64) {
	if alpha <= 0 {
		return
	}
	b := dst.NRGBAAt(x, y)
	mix := func(f, g uint8) uint8 { return uint8(float64(f)*alpha + float64(g)*(1-alpha) + 0.5) }
	a := alpha + float64(b.A)/255*(1-alpha)
	dst.SetNRGBA(x, y, color.NRGBA{mix(col.R, b.R), mix(col.G, b.G), mix(col.B, b.B), uint8(a*255 + 0.5)})
}

// trayTooltip names the app, the active server, and the state.
func trayTooltip(m core.TrayModel) string {
	server := m.Server
	if server == "" {
		server = locales.TrayNoServer()
	}
	state := locales.TrayMonitoring()
	switch m.Icon {
	case core.TrayPaused:
		state = locales.TrayPaused()
	case core.TrayUploading:
		state = locales.TrayUploading()
	case core.TrayError:
		state = locales.TrayFailed()
	}
	return locales.TrayTooltip(locales.AppName(), server, state)
}

// trayActions are what the tray menu's items do.
type trayActions struct {
	setMonitoring func(on bool)
	uploadNow     func()
	setActive     func(id int64)
	open          func()
	quit          func()
}

// trayMenu builds the native menu of a model.
func trayMenu(m core.TrayModel, act trayActions) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, it := range m.Items {
		switch it.Kind {
		case core.TrayItemPause:
			items = append(items, &mygo.MenuItem{Label: locales.TrayPause(),
				Click: func(*mygo.MenuItem, *mygo.Window) { act.setMonitoring(false) }})
		case core.TrayItemResume:
			items = append(items, &mygo.MenuItem{Label: locales.TrayResume(),
				Click: func(*mygo.MenuItem, *mygo.Window) { act.setMonitoring(true) }})
		case core.TrayItemUploadNow:
			items = append(items, &mygo.MenuItem{Label: locales.TrayUploadNow(),
				Click: func(*mygo.MenuItem, *mygo.Window) { act.uploadNow() }})
		case core.TrayItemServers:
			var sub []*mygo.MenuItem
			for _, s := range it.Submenu {
				id := s.ProfileID
				sub = append(sub, &mygo.MenuItem{Label: s.Label, Type: mygo.MenuItemRadio, Checked: s.Checked,
					Click: func(*mygo.MenuItem, *mygo.Window) { act.setActive(id) }})
			}
			items = append(items, &mygo.MenuItem{Label: locales.TrayServers(), Submenu: sub, Disabled: len(sub) == 0})
		case core.TrayItemSeparator:
			items = append(items, mygo.Separator())
		case core.TrayItemOpen:
			items = append(items, &mygo.MenuItem{Label: locales.TrayOpen(),
				Click: func(*mygo.MenuItem, *mygo.Window) { act.open() }})
		case core.TrayItemQuit:
			items = append(items, &mygo.MenuItem{Label: locales.TrayQuit(),
				Click: func(*mygo.MenuItem, *mygo.Window) { act.quit() }})
		}
	}
	return items
}
