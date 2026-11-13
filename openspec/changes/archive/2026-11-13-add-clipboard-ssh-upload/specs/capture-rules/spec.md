# Spec Delta

## Purpose

Lets the user decide, with an ordered list of matching rules, which captured clipboard content is uploaded and where it goes, so private or irrelevant clipboard data never leaves the machine.

## ADDED Requirements

### Requirement: Rules evaluate per entry
The system SHALL evaluate rules separately for each upload entry: the image of an image item, the text of a text item, and each file of a files item. Entries from one files item can therefore get different outcomes.

#### Scenario: Mixed files
- **WHEN** a files item contains `photo.jpg` and `setup.exe`, and a rule skips extension `exe`
- **THEN** `photo.jpg` is uploaded and `setup.exe` is recorded as skipped with that rule's name

### Requirement: Ordered first-match evaluation
Rules SHALL be evaluated in the user's order, from top to bottom. Disabled rules are ignored. The first enabled rule whose conditions all match decides the entry's outcome. If no rule matches, the configured default action (upload or skip, initially upload) applies.

#### Scenario: Earlier rule wins
- **WHEN** rule 1 uploads `png` files to `~/shots`, rule 2 skips all images, and an image entry is captured
- **THEN** the entry is uploaded to `~/shots`

#### Scenario: No rule matches
- **WHEN** no enabled rule matches a captured entry and the default action is skip
- **THEN** the entry is recorded as skipped with reason "no matching rule"

### Requirement: Match conditions
A rule SHALL support these conditions, combined with AND: content kinds (any of image, file, text); extensions (any of a list, case-insensitive, without dot); filename glob, case-insensitive with `*` and `?`; minimum and maximum size in bytes; minimum and maximum image width and height; text regex. An unset condition matches everything.

#### Scenario: Extension and size
- **WHEN** a rule requires kind file, extensions `log, txt`, and maximum size 10 MB, and the user copies a 2 MB `server.LOG`
- **THEN** the rule matches

#### Scenario: Tiny image filtered
- **WHEN** a rule skips images narrower than 32 pixels, and the user copies a 16×16 icon
- **THEN** the image is recorded as skipped with that rule's name

#### Scenario: Text regex
- **WHEN** a rule with kind text and regex `^https?://` is set to upload, and the user copies `https://example.com/a`
- **THEN** the rule matches and the text is uploaded

#### Scenario: Condition does not apply to kind
- **WHEN** a rule sets only a text regex and an image entry is evaluated
- **THEN** the rule does not match the image entry

### Requirement: Image and text entry names
For filename and extension conditions, image entries SHALL use the original name `image` with extension `png`, and text entries SHALL use `text` with extension `txt`.

#### Scenario: Glob on an image entry
- **WHEN** a rule has filename glob `image.*` and an image entry is evaluated
- **THEN** the glob condition matches

### Requirement: Rule actions and overrides
A rule's action SHALL be upload or skip. An upload rule can optionally override the remote directory and the filename template. The overriding directory can be absolute, start with `~/`, or be relative to the active profile's default directory.

#### Scenario: Relative directory override
- **WHEN** the active profile's directory is `~/clipboard` and an upload rule for kind image sets directory `screenshots/{date}`
- **THEN** a matching image captured on 2026-11-12 is uploaded under `~/clipboard/screenshots/2026-11-12/`

#### Scenario: Template override
- **WHEN** an upload rule for extension `log` sets filename template `{name}-{datetime}.{ext}` and `app.log` is copied
- **THEN** the remote filename has the form `app-20261112-153045.log`

### Requirement: Rule management
The system SHALL let the user create, edit, enable or disable, reorder, and delete rules, and set the default action. A rule SHALL NOT be saved with an invalid regex, a minimum greater than its maximum, or an unparseable template. Changes apply to the next captured entry.

#### Scenario: Invalid regex rejected
- **WHEN** the user saves a rule with text regex `([a-z`
- **THEN** the rule is not saved and the regex field shows the parse error

#### Scenario: Reorder applies immediately
- **WHEN** the user moves a skip rule above an upload rule that matches the same entries
- **THEN** the next matching entry is skipped

### Requirement: Default rules on first run
On first run, the system SHALL create two enabled rules, in order: "Skip text" (kind text, action skip) and "Skip large files" (kind file, minimum size 100 MB, action skip). The default action SHALL be upload.

#### Scenario: Fresh install
- **WHEN** the app starts with no existing configuration and the user copies text
- **THEN** the text is recorded as skipped by "Skip text", and a copied screenshot is uploaded

### Requirement: Test rules against the clipboard
The system SHALL provide a "Test with current clipboard" action. It shows, for each entry on the clipboard, which rule would match, the resulting action, and the resolved remote path, without uploading or recording anything.

#### Scenario: Dry run
- **WHEN** the user copies `notes.md` and runs "Test with current clipboard"
- **THEN** the app shows `notes.md`, the matching rule (or "default action"), the action, and the remote path it would upload to, and history is unchanged
