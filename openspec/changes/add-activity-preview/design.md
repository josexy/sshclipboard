# Design

## Context

The Activity page (`internal/view/activity.go`) builds the history as a `ui.Table` over `activityVM.list` (a `ui.ListState`) with single selection in `activityVM.selected`. `list.Key` is the entry ID, so the selection follows its entry as rows arrive and becomes -1 when the entry leaves the page. `list.Label` turns on MyGo's type-to-select.

Text and image entries point at files the app staged (`HistoryEntry.LocalPath`, `Staged`): `<staging>/<yyyy-mm>/<id>.txt` or `.png`, up to 64 MiB (`capture.MaxBytes`). Entries skipped at capture (too large, confidential) have no staged file, and retention or Delete removes staged files. Width, height, and size are already in `HistoryEntry`.

`thumbs.go` already reads staged files from the view: `thumbCache` reads and decodes off the main thread and publishes through `redraw` (`Window.Update`). The Controller isn't involved.

These are the pinned MyGo v0.9.0 behaviors this design relies on:

- With the focus in a scrollable list, Space pages it (`rt.scrollKey`). After a letter typed within a second, Space extends type-to-select. Both happen only when no element around the focus claims Space as a shortcut. `Handle.OnShortcut` and `Element.OnShortcut` claim the key for their focus subtree.
- `c.ShortcutRepeated()` tells an auto-repeated key from a new press, in action callbacks too.
- `ui.Modal` dims the window, closes on Escape or an outside click, caps its panel at the window size minus margins, and records the focused element so focus can return there on close (`restoreFocus`).
- A read-only text input's editor takes only Ctrl+A, Ctrl+C, and Shift with the arrow keys or Home/End (`editor.wants`). Space and the plain arrows go on to shortcuts, or else to scrolling. `ui.TextAreaBuffer` lays out only the paragraphs in view.
- The window's clipboard watcher ignores changes whose clipboard owner is the app's own process (`OwnedBySelf`). A Ctrl+C from a MyGo text control is one of those.

## Goals / Non-Goals

**Goals:**
- Meet the spec delta in `specs/upload-history/spec.md` within `internal/view`, without new Controller commands, Platform fields, store changes, or settings.
- Never read or decode a file on the UI thread, and never show a stale result.
- Keep at most one full-size preview in memory.

**Non-Goals:**
- Caching previews across opens, prefetching neighbors, zoom, or pan.
- Previewing files, or text encodings other than UTF-8 (capture stages UTF-8 only).
- A native preview window. The user chose the in-app dialog.

## Decisions

### D1. An in-app `ui.Modal`, built by the Activity page

The dialog is a `ui.Modal` built at the end of `activityPage`, next to the Clear history `AlertDialog`, while `activityVM.previewOpen` is true. It has a header (kind icon, original name, a Close icon button), a body, and a footer with metadata (dimensions and size, or size) and a muted hint, "Space or Esc to close · ↑↓ to browse". The panel takes the modal's maximum size, and the body grows into it. All colors come from `view.Palette` and the theme.

Considered: a second `mygo.Window` with the main window as `Parent`. It would need a new `view.Platform` hook, wiring in `main.go`, and a second view and state per window, and `ui.Tester` couldn't cover it. The user chose the dialog.

### D2. The dialog follows the table's selection

The dialog has no entry of its own. Each build reads `rows[vm.selected]` from the current `State.History`:

- `vm.selected` is -1 (the entry was deleted or left the page, or the page changed): close the dialog.
- File kind: show the "no preview for files" note.
- Text or image: show the loader's content for the entry, or "Loading…" until it's ready.

Browsing then needs only to move the selection, and deletion, paging, and new arrivals follow from the table's existing key-tracking. Nothing else has to keep the two in sync.

### D3. A view-level `previewLoader` in `internal/view/preview.go`, modeled on `thumbCache`

`previewLoader.get(e store.HistoryEntry) (previewContent, bool)` returns the content when it's ready for the key `(e.ID, e.LocalPath)`. Otherwise it starts one goroutine for that key, unless one is already running, and returns false. Results go through `redraw`. A result is kept only if its key is still the wanted key, and keeping it drops the previous content, so at most one preview is held. `reset()` on close releases it. Calling `get` again during the same build is harmless, as with `thumbCache.get`.

`previewContent` carries a kind, an optional `*ui.Bitmap`, an optional `*ui.TextBuffer`, a truncated flag, and an unavailable flag. The read function is a field, so tests can replace it, and tests drive `redraw` themselves.

Considered: a `Controller.Preview(id, done)` command. The Controller's single loop goroutine also handles upload progress and captures, so decoding a 40 MB PNG there would stall those updates. The view already has the path, as it does for thumbnails.

### D4. Reading content

- **Text:** open the file and read at most `previewTextLimit` (1 MiB) plus 3 bytes. If the file is longer, cut back to the last complete UTF-8 sequence at or before 1 MiB and set `truncated`. Replace invalid sequences with `strings.ToValidUTF8(…, "�")`. Fill a `ui.TextBuffer` off the main thread; its methods are goroutine-safe. A missing or unreadable file sets `unavailable`.
- **Image:** check the header with `png.DecodeConfig`. If the long side is at most `previewMaxSide` (4096 px), use `ui.DecodeBitmap(data)`. Otherwise decode, scale down with `golang.org/x/image/draw` keeping the aspect ratio, and use `ui.NewBitmap`. This keeps the bitmap under GPU texture limits (16384 on D3D11) and under 64 MiB of RGBA. The footer shows the entry's original `Width × Height`, not the scaled size.

### D5. Image layout

`ui.Image(c, bmp).Fit(ui.Contain)`, with `MaxWidth` and `MaxHeight` set to the bitmap's own size in DIPs (`ui.ImageSize`), inside a centered, growing box on `pal` surface color. Large images scale down to fit, and small ones keep their size. A scaled-down bitmap keeps the original's aspect ratio, so `Contain` shows it correctly. The image gets the original name as its accessibility label.

### D6. Text layout

`ui.TextAreaBuffer(c, &content.text).ReadOnly(true)` grows to fill the body and has the original name as its label. Being read-only, it can still be focused, selected, and copied, and Tab reaches it. A truncated preview shows a `RoleStatus` note above it: "Showing the first 1 MB of 5 MB".

Considered: `ui.Text(…).Selectable()` in a `ui.Scroll`. That lays out the whole megabyte every time the layout changes.

### D7. Keys

- **Open:** in `activityPage`, after the table is built, call `vm.list.OnShortcut(c, 0, ui.KeySpace, …)`, which goes through the embedded `Handle`. The callback ignores repeats (`c.ShortcutRepeated()`). It opens the preview only when `vm.selected` is a text or image row. For other rows it does nothing, but Space stays claimed, so the table doesn't page.
- **In the dialog:** `c.OnShortcut` inside the modal's builder declares:
  - Space, ignoring repeats: closes the dialog.
  - Up and Down, repeats allowed: `vm.list.Choose(i)` and `vm.list.ScrollIntoView(i)` for the neighboring row, kept within `0 … len(rows)-1`.

  These claims cover the whole dialog, the read-only text included (see Context). Escape and outside clicks are the modal's own. Page Up, Page Down, Home, and End are not claimed, so they still scroll the text.
- **Initial focus:** the Close button, using `AutoFocus`. A focused button takes Space as a click before shortcuts, and that also closes the dialog.
- **Return focus:** on every close path (Space, Close, Escape, outside click, or the selection becoming -1), set `previewOpen = false`, reset the loader, and call `vm.list.Focus(c)` (bind the table with `.Bind(&vm.list.Handle)` if it isn't bound already). The modal's opener restore isn't enough after browsing, because the opener can be a row element other than the selected one.

### D8. Context-menu item

`activityEntryMenu` adds Preview first, with a new `eye` Lucide icon added unchanged to `internal/view/icons` and declared in `icons.go`. It appears for text and image entries only. It sets `vm.selected` to the row through `vm.list.Choose`, then opens the dialog, so D2 applies unchanged.

### D9. Strings

New `locales/en.json` keys, regenerated into `catalog_gen.go`:
- `actionPreview`: "Preview"
- `previewClose`: "Close preview"
- `previewHint`: "Space or Esc to close · ↑↓ to browse"
- `previewLoading`: "Loading…"
- `previewNoFiles`: "Files have no preview"
- `previewUnavailable`: "This content is no longer available"
- `previewTruncated` ({shown}, {total}): "Showing the first {shown} of {total}"
- `previewDimensions` ({w}, {h}): "{w} × {h}"

## Risks / Trade-offs

- [Space no longer extends type-to-select in the Activity table, so multi-word prefixes can't be typed] → Entry names rarely contain spaces in their first letters ("image.png", "text.txt", file names). Single letters and prefixes without spaces still work.
- [Decoding a 64 MiB PNG briefly holds the encoded bytes and a full RGBA decode, about 256 MB for 8K × 8K] → Only one load is wanted at a time. Stale results are dropped and released when they arrive. `previewMaxSide` bounds what's kept. Fast browsing can still overlap a few decodes, which is acceptable for a bounded burst.
- [The user drags a scroll bar and presses Space while hovering the table without focus] → Space acts only when the table has the focus, which the spec states.
- [Development builds on Linux and macOS use the fallback watcher, which can capture a Ctrl+C from the preview] → Windows is the target and checks clipboard ownership. The manual test checks this on Windows.
- [The modal's maximum size depends on the window size (minimum 860 × 560)] → Tests build the dialog at the minimum and the default size.
