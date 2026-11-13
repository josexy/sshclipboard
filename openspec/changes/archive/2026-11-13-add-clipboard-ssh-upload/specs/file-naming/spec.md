# Spec Delta

## Purpose

Defines how the remote filename and remote directory of each uploaded entry are produced from user templates, so names are predictable, collision-resistant, and safe on Linux filesystems and shells.

## ADDED Requirements

### Requirement: Filename templates per kind
The system SHALL keep one filename template each for images, files, and text. Defaults: images `screenshot_{datetime}.{ext}`, files `{name}.{ext}`, text `clip_{datetime}.{ext}`. A matching upload rule's template override replaces the kind's template for that entry.

#### Scenario: Default image name
- **WHEN** an image is captured at local time 2026-11-12 15:30:45 with default templates
- **THEN** its remote filename is `screenshot_20261112-153045.png`

#### Scenario: Default file name
- **WHEN** the file `Quarterly Report.pdf` is uploaded with default templates
- **THEN** its remote filename is `Quarterly Report.pdf`

### Requirement: Template tokens
Templates SHALL support these tokens: `{name}`, `{ext}`, `{date}` (`YYYY-MM-DD`), `{time}` (`HHmmss`), `{datetime}` (`YYYYMMDD-HHmmss`), `{date:FORMAT}`, `{unix}`, `{counter}`/`{counter:N}`, `{rand}`/`{rand:N}`, `{hash}`/`{hash:N}`, `{kind}`, `{profile}`, and `{host}`. `{{` and `}}` produce literal braces. All times are the entry's local capture time.

#### Scenario: Custom date format
- **WHEN** the template is `{date:YYYY/MM/DD_HH-mm-ss.SSS}_{name}.{ext}` and `a.txt` is captured at 2026-11-12 09:05:07.250
- **THEN** the `/` characters are replaced as unsafe, and the filename is `2026_11_12_09-05-07.250_a.txt`

#### Scenario: Hash and random
- **WHEN** the template is `{hash:8}-{rand:4}.{ext}` for an image
- **THEN** the filename has the first 8 hex characters of the content's SHA-256, a hyphen, 4 lowercase alphanumeric characters, and `.png`

#### Scenario: Literal braces
- **WHEN** the template is `{{draft}}_{name}.{ext}` for `x.md`
- **THEN** the filename is `{draft}_x.md`

### Requirement: Token meanings
`{name}` SHALL be the original base name without extension (`image` for images, `text` for text). `{ext}` SHALL be the original extension without the dot (`png` for images, `txt` for text). `{counter}` SHALL be a per-profile, per-local-day sequence starting at 1, zero-padded to N digits (default 3), that persists across restarts. `FORMAT` SHALL accept `YYYY`, `YY`, `MM`, `DD`, `HH`, `mm`, `ss`, `SSS`, and literal text.

#### Scenario: Counter resets daily
- **WHEN** the template is `shot_{counter}.{ext}`, two images were uploaded to profile "dev-box" earlier today, and one more is captured
- **THEN** the new filename is `shot_003.png`, and the first image captured tomorrow is `shot_001.png`

#### Scenario: Counter survives restart
- **WHEN** the app restarts after `shot_003.png` was produced today for "dev-box"
- **THEN** the next image that day for "dev-box" is `shot_004.png`

### Requirement: Extension is kept
When a filename template has no `{ext}` token and the entry has an extension, the system SHALL append `.` followed by the extension to the rendered name.

#### Scenario: Template without extension
- **WHEN** the image template is `img_{datetime}`
- **THEN** the filename ends with `.png`

### Requirement: Name transforms
Each template SHALL have two optional transforms, both off by default: lowercase the whole name, and replace spaces with `_`. Transforms are applied after the template is rendered and before sanitization.

#### Scenario: Lowercase and underscores
- **WHEN** both transforms are on for files and `My Report.PDF` is uploaded with template `{name}.{ext}`
- **THEN** the filename is `my_report.pdf`

### Requirement: Sanitize names for Linux
The system SHALL make every rendered filename safe: replace `/`, `\`, NUL, control characters, and `: * ? " < > |` with `_`; trim leading and trailing spaces and dots; replace a leading `-` with `_`; and shorten the base name so the name is at most 255 bytes in UTF-8 with its extension kept. An empty result becomes `clipboard_{datetime}` with the entry's extension.

#### Scenario: Leading dash and slash
- **WHEN** a template renders the name `-rf: a/b.txt`
- **THEN** the filename is `_rf_ a_b.txt`

#### Scenario: Overlong name
- **WHEN** the rendered name is 300 bytes ending in `.png`
- **THEN** the filename is at most 255 bytes and still ends in `.png`

### Requirement: Directory templates
A profile's default remote directory and a rule's directory override SHALL accept the tokens `{date}`, `{time}`, `{datetime}`, `{date:FORMAT}`, `{unix}`, `{kind}`, `{profile}`, and `{host}`. `/` separates path segments. Each segment is sanitized like a filename, and a template that produces a `..` segment SHALL be rejected.

#### Scenario: Dated folders
- **WHEN** the profile directory is `~/clipboard/{date:YYYY-MM}` and an entry is captured on 2026-11-12
- **THEN** the entry is uploaded under `~/clipboard/2026-11`

#### Scenario: Traversal rejected
- **WHEN** the user saves a directory template `~/clipboard/../../etc`
- **THEN** the template is rejected with an error and not saved

### Requirement: Template validation and preview
The system SHALL reject templates with unknown tokens, unbalanced braces, or invalid `FORMAT` or `N` arguments, and show the error position. While the user edits a template, the system SHALL show a live preview of the resulting name for sample entries of each kind.

#### Scenario: Unknown token
- **WHEN** the user enters the template `{nmae}.{ext}`
- **THEN** the template is not saved, and the error says `{nmae}` is an unknown token

#### Scenario: Live preview
- **WHEN** the user types `{profile}_{name}.{ext}` in the files template while profile "dev-box" is active
- **THEN** the preview shows `dev-box_example.txt`
