# Capture rules

Rules decide which clipboard content is uploaded and where it goes. You
manage them on the **Rules** page.

## What a rule is checked against

Rules are checked separately for each **entry**:

- an image item has one entry, the image;
- a text item has one entry, the text;
- a files item has one entry per file, so one copy can upload some files
  and skip others.

Folders in a file list are never uploaded: each one is recorded as
skipped with "folders are not supported".

## Evaluation order

Rules are checked from top to bottom. Disabled rules are ignored. The
**first** enabled rule whose conditions all match decides what happens to
the entry, and later rules are not checked. When no rule matches, the
**default action** applies (upload, unless you change it). An entry that the
default action skips is recorded with "no matching rule".

Reordering takes effect for the next entry captured. Use drag and drop,
or the **Move up** and **Move down** buttons.

"Upload clipboard now" (the hotkey, the tray menu, or the button in the
window) skips the rules entirely and uploads with the server's default
folder and the default naming templates. So does **Upload anyway** on a
skipped entry in Activity.

## Conditions

A rule matches when **all** of its set conditions match. A condition you
leave empty matches everything.

| Condition | Matches when |
|-----------|--------------|
| Content kinds | The entry is any of the chosen kinds: image, file, text. |
| Extensions | The extension is any of the list, without the dot, ignoring case (`log, txt` matches `server.LOG`). |
| File name | The name matches a pattern, ignoring case: `*` is any run of characters and `?` is one character. |
| Size | The size in bytes is at least the minimum and at most the maximum. |
| Image width and height | The image is at least the minimum and at most the maximum, in pixels. |
| Text | The text matches a regular expression (RE2 syntax, as in Go). |

Images and text have no file name, so for the extension and file name
conditions an image is called `image.png` and text is called `text.txt`.

Some conditions only make sense for one kind. A rule with image width or
height conditions never matches files or text, and a rule with a text
pattern never matches images or files.

A rule can't be saved with an invalid regular expression or file name
pattern, a minimum greater than its maximum, or an invalid template.

## Actions

A rule either **uploads** or **skips** the entry. An upload rule can also
override:

- **Folder**: where the entry goes. A folder starting with `/` is
  absolute, one starting with `~/` is in your home directory on the server,
  and any other is inside the server's default folder. Folders take the
  directory tokens of [naming.md](naming.md#directories), such as `{date}`.
- **File name template**: replaces the template of the entry's kind (see
  [naming.md](naming.md)).

## Defaults

A new installation has two rules, in this order, and the default action
**upload**:

1. **Skip text**: content kind text, skip.
2. **Skip large files**: content kind file, minimum size 100 MB, skip.

So screenshots and ordinary files upload, while copied text and very large
files stay on your computer until you upload them yourself.

## Test with current clipboard

**Test with current clipboard** on the Rules page shows, for each entry on
the clipboard, the rule that would match (or "default action"), the
action, and the remote path it would be uploaded to. Nothing is uploaded
or recorded. It runs the same code as real captures, so what it shows is
what will happen.

## Examples

Every row is checked by `TestDocsRulesExamples` in
`internal/rules/docs_test.go`. A rule is written as its action and its
conditions: `kind=`, `ext=`, `glob=`, `min=`/`max=` (size), `maxwidth=`,
`regex=`, and `dir=` for a folder override. Rules are listed top to bottom,
separated by `;`. The server's default folder is `~/clipboard`, and
entries are captured on 2026-11-12.

| Rules | Default | Entry | Result |
|-------|---------|-------|--------|
| `skip ext=exe` | upload | file `photo.jpg` | upload, default action |
| `skip ext=exe` | upload | file `setup.exe` | skip, rule 1 |
| `upload ext=png dir=~/shots; skip kind=image` | upload | image 1280×720 | upload, rule 1, `~/shots` |
| `skip ext=exe` | skip | file `notes.md` | skip, default action |
| `upload kind=file ext=log,txt max=10MB` | skip | file `server.LOG` 2 MB | upload, rule 1 |
| `skip kind=image maxwidth=31` | upload | image 16×16 | skip, rule 1 |
| `upload kind=text regex=^https?://` | skip | text `https://example.com/a` | upload, rule 1 |
| `skip regex=.` | upload | image 1280×720 | upload, default action |
| `skip glob=image.*` | upload | image 1280×720 | skip, rule 1 |
| `upload kind=image dir=screenshots/{date}` | upload | image 1280×720 | upload, rule 1, `~/clipboard/screenshots/2026-11-12` |
| `skip glob=*.pdf; upload ext=pdf` | upload | file `report.pdf` | skip, rule 1 |
| `skip kind=text; skip kind=file min=100MB` | upload | text `hello world` | skip, rule 1 |
| `skip kind=text; skip kind=file min=100MB` | upload | image 1280×720 | upload, default action |
| `skip kind=text; skip kind=file min=100MB` | upload | file `disk.iso` 500 MB | skip, rule 2 |
