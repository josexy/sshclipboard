package view

import (
	"embed"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/store"
)

// The icons are Lucide's (https://lucide.dev), copied unchanged from
// lucide-icons/lucide at commit a630cec77eac6568b989e87e4947a8bfe45caacd,
// under the ISC license in icons/LICENSE. To add one, copy its SVG from the
// same commit into icons/ and declare it below.
//
//go:embed icons/*.svg
var iconFiles embed.FS

// lucide parses an embedded icon. It panics on a missing or broken file,
// which every test of the package would show.
func lucide(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

// The icons the views show, parsed once. They are shared: an element
// sizes and colors them, never the SVG.
var (
	iconActivity        = lucide("activity")
	iconArrowDown       = lucide("arrow-down")
	iconArrowUp         = lucide("arrow-up")
	iconBan             = lucide("ban")
	iconBraces          = lucide("braces")
	iconCheck           = lucide("check")
	iconChevronLeft     = lucide("chevron-left")
	iconChevronRight    = lucide("chevron-right")
	iconCircleCheck     = lucide("circle-check")
	iconCircleDot       = lucide("circle-dot")
	iconCircleMinus     = lucide("circle-minus")
	iconCircleX         = lucide("circle-x")
	iconClipboardCopy   = lucide("clipboard-copy")
	iconClipboardList   = lucide("clipboard-list")
	iconClock           = lucide("clock")
	iconCloudUpload     = lucide("cloud-upload")
	iconCopy            = lucide("copy")
	iconExternalLink    = lucide("external-link")
	iconFile            = lucide("file")
	iconFilePen         = lucide("file-pen")
	iconFlaskConical    = lucide("flask-conical")
	iconFolderOpen      = lucide("folder-open")
	iconGripVertical    = lucide("grip-vertical")
	iconHardDriveUpload = lucide("hard-drive-upload")
	iconImage           = lucide("image")
	iconKeyRound        = lucide("key-round")
	iconListFilter      = lucide("list-filter")
	iconPencil          = lucide("pencil")
	iconPlugZap         = lucide("plug-zap")
	iconPlus            = lucide("plus")
	iconRotateCw        = lucide("rotate-cw")
	iconSave            = lucide("save")
	iconSearchX         = lucide("search-x")
	iconServer          = lucide("server")
	iconSettings        = lucide("settings")
	iconTrash           = lucide("trash")
	iconTriangleAlert   = lucide("triangle-alert")
	iconType            = lucide("type")
	iconUpload          = lucide("upload")
	iconX               = lucide("x")
)

// iconSize is the side of an icon beside text of the theme's font size:
// 16 DIPs beside 14, growing with the desktop's text size.
func iconSize(c *ui.Context) float32 {
	return c.Theme().FontSize * 8 / 7
}

// icon shows an icon beside text, in the color of the text around it.
func icon(c *ui.Context, svg *ui.SVG) ui.Element {
	s := iconSize(c)
	return ui.Icon(c, svg).Size(s, s)
}

// withIcon fills b, a button made with an empty label, with an icon
// before label. Assistive technology names the button by the label.
func withIcon(c *ui.Context, b ui.Element, svg *ui.SVG, label string) ui.Element {
	return b.Children(func() {
		icon(c, svg)
		ui.Text(c, label).SingleLine()
	})
}

// button is ui.Button with an icon before its label.
func button(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	return withIcon(c, ui.Button(c, ""), svg, label)
}

// primaryButton is ui.PrimaryButton with an icon before its label.
func primaryButton(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	return withIcon(c, ui.PrimaryButton(c, ""), svg, label)
}

// iconButton is a button showing only an icon: label names it for
// assistive technology, and tip shows as the pointer rests on it.
func iconButton(c *ui.Context, svg *ui.SVG, label, tip string) ui.Element {
	return ui.Button(c, "").Label(label).Tooltip(tip).Children(func() { icon(c, svg) })
}

// iconText shows an icon before text in one color, as a status message.
func iconText(c *ui.Context, svg *ui.SVG, text string, color ui.Color) ui.Element {
	return ui.Row(c).Gap(6).AlignItems(ui.Center).TextColor(color).Children(func() {
		icon(c, svg).Shrink(0)
		ui.Text(c, text).Wrap()
	})
}

// statusIcon is the icon of a history status, shown in its StatusColor.
func statusIcon(s store.Status) *ui.SVG {
	switch s {
	case store.StatusQueued:
		return iconClock
	case store.StatusUploading:
		return iconCloudUpload
	case store.StatusUploaded:
		return iconCircleCheck
	case store.StatusFailed:
		return iconCircleX
	case store.StatusCancelled:
		return iconBan
	}
	return iconCircleMinus
}

// kindIcon is the icon of a kind of content.
func kindIcon(k store.Kind) *ui.SVG {
	switch k {
	case store.KindImage:
		return iconImage
	case store.KindFile:
		return iconFile
	}
	return iconType
}

// entryMenuIcons are the icons of an Activity entry's menu. Each menu icon
// is drawn to a bitmap when made, so they're made once, at first use.
var entryMenuIcons = sync.OnceValue(func() entryIcons {
	return entryIcons{
		copy: menuIcon(iconCopy), retry: menuIcon(iconRotateCw), upload: menuIcon(iconUpload),
		cancel: menuIcon(iconX), open: menuIcon(iconExternalLink), folder: menuIcon(iconFolderOpen),
		delete: menuIcon(iconTrash),
	}
})

type entryIcons struct {
	copy, retry, upload, cancel, open, folder, delete ui.MenuIcon
}

// menuIcon makes a template menu icon, which menus show in the color of
// their text; nil, which shows none, if it can't be made.
func menuIcon(svg *ui.SVG) ui.MenuIcon {
	ic, err := mygo.NewMenuIconSVG(svg, true)
	if err != nil {
		return nil
	}
	return ic
}
