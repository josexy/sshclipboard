# Tasks

## 1. Preview loader (`internal/view/preview.go`)

- [x] 1.1 Add `previewContent` and a text reader that reads at most 1 MiB (+3 bytes) of a staged `.txt`, cuts back to a complete UTF-8 sequence, replaces invalid bytes with U+FFFD, sets `truncated` and the file's total size, and marks a missing or unreadable file `unavailable` (design D4). Verify with table tests in `preview_test.go`: short text, exactly 1 MiB, 1 MiB + a multi-byte rune straddling the cut, invalid UTF-8, empty file, and a missing file
- [x] 1.2 Add the image reader: `png.DecodeConfig`, then `ui.DecodeBitmap` when the long side is ≤ 4096 px, else decode and scale down with `golang.org/x/image/draw` to a 4096 px long side, keeping the aspect ratio (design D4). Verify with tests on generated PNGs: 64×64 kept at 64×64, 8000×2000 scaled to 4096×1024, a corrupt PNG and a missing file `unavailable`
- [x] 1.3 Add `previewLoader` with `get(entry)`, keyed by `(ID, LocalPath)`, one goroutine per wanted key, results through `redraw`, stale results dropped, the previous content released, and `reset()`. The read function is a field tests can replace (design D3). Verify with tests: a result arrives after `redraw` runs; with key A then B wanted, A's late result is dropped and B is kept; repeated `get` during a load starts one read; `reset` drops the content; and `go test -race ./internal/view` passes

## 2. Strings and icon

- [x] 2.1 Add the D9 keys to `locales/en.json` (`actionPreview`, `previewClose`, `previewHint`, `previewLoading`, `previewNoFiles`, `previewUnavailable`, `previewTruncated`, `previewDimensions`), with `@` descriptions for the placeholders, and run `go tool mygo generate`. Verify that `go tool mygo i18n check .` and `go test ./locales` pass
- [x] 2.2 Copy Lucide's `eye.svg` unchanged into `internal/view/icons`, declare it in `icons.go`, and add a `preview` field to `entryIcons` and `entryMenuIcons`. Verify that `TestIconsDeclaredAndDrawn` passes and that `TestEntryMenuIcons`, extended with `ic.preview`, passes

## 3. Preview dialog and keys (`internal/view/activity.go`)

- [x] 3.1 Add `previewOpen` to `activityVM` and a `previewLoader` to `App` (created in `NewApp` with `redraw`). Build the `ui.Modal` at the end of `activityPage`, following the table's selection: close when `vm.selected` is -1, show the files note for file rows, loading, unavailable, the image (Contain, capped at its own size), or a read-only `TextAreaBuffer` with the truncation note, plus a header with name and Close and a footer with dimensions and size and the hint (design D1, D2, D5, D6). Verify with `ui.Tester` tests in `activity_test.go` using a stub reader: image and text content, loading then content after `redraw`, the files note, the unavailable note, the truncation note, and layout at 860×560 and 1100×720 without the footer clipped
- [x] 3.2 Claim Space on the history table through `vm.list.OnShortcut`: open on a non-repeated Space for text and image rows only, and claim it but do nothing otherwise (design D7). Verify with tests for the spec scenarios "Preview a screenshot", "Preview copied text", "File entry not previewed" (no dialog, the table's scroll offset unchanged), "Nothing selected", and "Holding Space" (`KeyDown` + `KeyRepeat` opens once)
- [x] 3.3 Inside the modal, declare non-repeated Space to close, and Up and Down (repeats allowed) to `Choose` and `ScrollIntoView` the neighboring row, clamped to the page. Autofocus Close, and on every close path reset the loader and focus the table through `vm.list` (design D7). Verify with tests for "Next entry", "Browse onto a file entry", "Edge of the page", "Fast browsing" (stub reads finished out of order show only the final row), "Space closes", "Escape closes", "Keyboard-only copy" (Tab into the text, Ctrl+A, Ctrl+C), Up and Down browsing while the text has focus, and that after closing, the focused element is the table with the previewed row selected
- [x] 3.4 Handle the selection changing under the dialog. Verify with tests for "Entry leaves the page" (delete the entry from fake `State.History`, and push it off the page with arrivals): the dialog closes and the table has the focus. Verify "New capture while previewing" (an arrival above it keeps the same entry shown) and that a page change closes the dialog
- [x] 3.5 Add **Preview** as the first item of `activityEntryMenu` for text and image entries only. It chooses the row and opens the dialog (design D8). Verify with tests for "Preview from the menu" and "No Preview for files", and that existing menu tests (`activity_test.go`) still pass
- [x] 3.6 Update the keyboard-order test (`keyboard_test.go`) if the dialog adds focus stops, and add a check that Tab cycles within the open dialog (Close, then the text body for text) without reaching the page behind it. Verify that `go test ./internal/view` passes

## 4. Documentation

- [x] 4.1 In the README's **What it does**, extend the history bullet to mention previewing text and images with Space. Verify that `go test -run TestReadme .` passes
- [x] 4.2 Add item 20 to `docs/manual-test.md`: preview a 4K screenshot and a long text with Space, browse with Up and Down onto a file entry, select and copy text with Ctrl+C (not captured as a new entry), close with Space and Esc, and check rendering at 150% display scale in light and dark. Verify that the new row matches the checklist table's columns

## 5. Verification

- [x] 5.1 Run `CGO_ENABLED=0 go test ./...`, `go test -race ./internal/view`, `go tool mygo vet .`, `gofmt -l .` (empty output), `GOOS=windows go vet ./...`, and `go tool mygo i18n check .`. Verify that all pass, with `-v` showing that the new preview tests ran rather than skipped
- [ ] 5.2 Run manual test item 20 on Windows 11 with a build from `go tool mygo build -platform windows/amd64`, and record the result in `docs/manual-test.md`'s table

## Workflow follow-up

- Archive the change with `/opsx:archive` after review, which merges the `upload-history` delta into `openspec/specs/upload-history/spec.md`.
