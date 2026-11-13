# File naming

SSHClipboard names each uploaded file from a **filename template**. There is
one template per kind of content, set on the **Naming** page:

| Kind   | Default template              | Example result                   |
|--------|-------------------------------|----------------------------------|
| Images | `screenshot_{datetime}.{ext}` | `screenshot_20261112-153045.png` |
| Files  | `{name}.{ext}`                | `Quarterly Report.pdf`           |
| Text   | `clip_{datetime}.{ext}`       | `clip_20261112-153045.txt`       |

An upload rule can override the template for the entries it matches (see
[rules.md](rules.md)). The Naming page shows a live preview of each template
for a sample entry: `image.png` for images, `example.txt` for files, and
`text.txt` for text, using the active server's name and host.

## Tokens

A token is a name in braces. All times are the local time the entry was
captured.

| Token            | Meaning                                                                 |
|------------------|-------------------------------------------------------------------------|
| `{name}`         | The original file name without its extension. `image` for images, `text` for text. |
| `{ext}`          | The original extension without the dot. `png` for images, `txt` for text. |
| `{date}`         | The date, `YYYY-MM-DD`.                                                 |
| `{time}`         | The time, `HHmmss`.                                                     |
| `{datetime}`     | The date and time, `YYYYMMDD-HHmmss`.                                   |
| `{date:FORMAT}`  | The date and time in a custom format (below).                          |
| `{unix}`         | Seconds since 1970-01-01 UTC.                                           |
| `{counter}`      | A per-server, per-day sequence starting at 1, padded to 3 digits. It survives restarts and starts again at 1 each day. |
| `{counter:N}`    | The counter padded to N digits (1–9).                                   |
| `{rand}`         | 6 random lowercase letters and digits.                                  |
| `{rand:N}`       | N random lowercase letters and digits (1–32).                           |
| `{hash}`         | The first 8 hex characters of the content's SHA-256.                    |
| `{hash:N}`       | The first N hex characters of the SHA-256 (1–64).                       |
| `{kind}`         | `image`, `file`, or `text`.                                             |
| `{profile}`      | The server profile's name.                                              |
| `{host}`         | The server's host name or address.                                      |
| `{{` and `}}`    | A literal `{` or `}`.                                                   |

The counter only advances when a name actually uses `{counter}`.

### Date formats

`FORMAT` in `{date:FORMAT}` is made of these fields and literal text. Any
character that is not a letter is literal; a letter that is not part of a
field is an error.

| Field  | Meaning                    |
|--------|----------------------------|
| `YYYY` | Year, 4 digits             |
| `YY`   | Year, 2 digits             |
| `MM`   | Month, 01–12               |
| `DD`   | Day, 01–31                 |
| `HH`   | Hour, 00–23                |
| `mm`   | Minute, 00–59              |
| `ss`   | Second, 00–59              |
| `SSS`  | Millisecond, 000–999       |

## The extension is kept

When a template has no `{ext}` token and the entry has an extension, `.` and
the extension are appended. With the image template `img_{datetime}`, the
name ends in `.png`.

## Transforms

Each template has two transforms, both off by default. They apply after the
template is rendered and before the name is made safe:

- **Lowercase**: lowercases the whole name.
- **Spaces to underscores**: replaces each space with `_`.

## Examples

Times are local. Every row below is checked by `TestDocsNamingExamples` in
`internal/naming/naming_test.go`.

| Template | Entry | Captured | Options | Result |
|----------|-------|----------|---------|--------|
| `screenshot_{datetime}.{ext}` | image | 2026-11-12 15:30:45 | | `screenshot_20261112-153045.png` |
| `{name}.{ext}` | `Quarterly Report.pdf` | 2026-11-12 15:30:45 | | `Quarterly Report.pdf` |
| `clip_{datetime}.{ext}` | text | 2026-11-12 15:30:45 | | `clip_20261112-153045.txt` |
| `{date:YYYY/MM/DD_HH-mm-ss.SSS}_{name}.{ext}` | `a.txt` | 2026-11-12 09:05:07.250 | | `2026_11_12_09-05-07.250_a.txt` |
| `shot_{counter}.{ext}` | image | 2026-11-12 15:30:45 | counter=3 | `shot_003.png` |
| `shot_{counter:5}.{ext}` | image | 2026-11-12 15:30:45 | counter=42 | `shot_00042.png` |
| `{hash:8}.{ext}` | image | 2026-11-12 15:30:45 | hash=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08 | `9f86d081.png` |
| `{{draft}}_{name}.{ext}` | `x.md` | 2026-11-12 15:30:45 | | `{draft}_x.md` |
| `img_{datetime}` | image | 2026-11-12 15:30:45 | | `img_20261112-153045.png` |
| `{name}.{ext}` | `My Report.PDF` | 2026-11-12 15:30:45 | lowercase, underscores | `my_report.pdf` |
| `{profile}_{name}.{ext}` | `example.txt` | 2026-11-12 15:30:45 | profile=dev-box | `dev-box_example.txt` |
| `{host}-{kind}-{date}.{ext}` | text | 2026-11-12 15:30:45 | host=10.0.0.5 | `10.0.0.5-text-2026-11-12.txt` |
| `{name}-{datetime}.{ext}` | `app.log` | 2026-11-12 15:30:45 | | `app-20261112-153045.log` |

`{rand:N}` gives a different value each time, so it has no fixed example:
`{hash:8}-{rand:4}.{ext}` gives names such as `9f86d081-k3x7.png`.

## Safe names

Every name is made safe for Linux filesystems and shells:

- `/`, `\`, NUL, control characters, and `: * ? " < > |` become `_`.
- Leading and trailing spaces and dots are removed.
- A leading `-` becomes `_`, so a name is never read as a command option.
- The name is shortened to at most 255 bytes (UTF-8), keeping the extension.
- A name that ends up empty becomes `clipboard_{datetime}` with the entry's
  extension.

| Rendered name | Result |
|---------------|--------|
| `-rf: a/b.txt` | `_rf_ a_b.txt` |
| ` ..notes.txt. ` | `notes.txt` |
| `a<b>c\|d.txt` | `a_b_c_d.txt` |
| `...` (a `.png` entry captured 2026-11-12 15:30:45) | `clipboard_20261112-153045.png` |

## Directories

A server's default remote directory and a rule's directory override are
templates too. They accept `{date}`, `{time}`, `{datetime}`,
`{date:FORMAT}`, `{unix}`, `{kind}`, `{profile}`, and `{host}`. `/` separates
folders, and each folder name is made safe like a filename. A directory
that starts with `~/` is in your home directory on the server, one that
starts with `/` is absolute, and any other is relative to your home
directory. A template that produces a `..` folder is rejected.

| Directory template | Captured | Result |
|--------------------|----------|--------|
| `~/clipboard/{date:YYYY-MM}` | 2026-11-12 15:30:45 | `~/clipboard/2026-11` |
| `/srv/in/{kind}` (an image) | 2026-11-12 15:30:45 | `/srv/in/image` |
| `~/clipboard/../../etc` | 2026-11-12 15:30:45 | rejected |
