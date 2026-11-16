# Proposal

## Why

The Activity list shows only a name ("image.png", "text.txt"), a 28-pixel thumbnail, and a size, so the user can't tell which screenshot or snippet an entry holds without opening the staged file in another app. A quick look from the keyboard, like Quick Look in Finder, makes it easy to find the right entry before copying its path, retrying, or uploading it anyway.

## What Changes

- Pressing **Space** while the Activity table has the focus and a **text** or **image** entry is selected opens a **preview dialog** over the main window. File entries (local files copied from Explorer) are not previewed, even when they are images or text.
- An image preview shows the staged PNG scaled to fit the dialog, with its pixel dimensions and size. A text preview shows the staged text in a scrollable, read-only area where it can be selected and copied, with its size. Very long text shows only its beginning and says so.
- While the preview is open, **Up** and **Down** move the selection to the previous or next row and the preview follows. A file row shows a "no preview for files" note instead of closing the dialog. An entry whose staged file is gone (pruned, deleted, or never staged) shows a "no longer available" note.
- **Space**, **Esc**, the **Close** button, or a click outside the dialog closes it. Focus returns to the table on the same row.
- Text and image rows get a **Preview** item in their context menu, so the preview works with the mouse too.
- Reading and decoding happen off the UI thread. A result that arrives after the user moved to another row is discarded.

The preview is read-only: it never changes history, the staged file, or the clipboard. Previewing files, a separate native preview window, zooming, and editing are out of scope.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `upload-history`: the Activity view gains a content preview for text and image entries, opened with Space or the context menu, with arrow-key browsing; the entry actions list gains Preview.

## Impact

- **Code**: `internal/view` (the preview dialog and its keyboard handling in `activity.go`, a new off-thread preview loader beside `thumbs.go`, the context-menu item, and tests), `locales/en.json` and the regenerated `locales/catalog_gen.go`, and possibly one new Lucide icon in `internal/view/icons`. `docs/manual-test.md` gains a preview check on a real desktop.
- **No changes** to `internal/core`, `view.Controller`, `internal/store`, the schema, or settings: the preview reads the staged file the history entry already points at.
- **Dependencies**: none new. Image scaling uses `golang.org/x/image/draw`, which capture already uses.
