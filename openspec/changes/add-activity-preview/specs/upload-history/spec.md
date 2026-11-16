# Spec Delta

## ADDED Requirements

### Requirement: Preview text and image entries
The Activity view SHALL open a preview dialog over the main window when the user presses Space while the history table has the focus and a text or image entry is selected. File entries SHALL NOT be previewed, whatever their content. The preview SHALL be read-only: it never changes history, the staged file, or the server.

#### Scenario: Preview a screenshot
- **WHEN** the history table has the focus, the selected entry is an image captured from the clipboard, and the user presses Space
- **THEN** a preview dialog opens showing that image

#### Scenario: Preview copied text
- **WHEN** the history table has the focus, the selected entry is a text entry, and the user presses Space
- **THEN** a preview dialog opens showing that text

#### Scenario: File entry not previewed
- **WHEN** the selected entry is a file entry, such as `diagram.png` copied from Explorer, and the user presses Space
- **THEN** no preview opens, and the table does not scroll

#### Scenario: Nothing selected
- **WHEN** the history table has the focus, no row is selected, and the user presses Space
- **THEN** no preview opens

#### Scenario: Holding Space
- **WHEN** the user holds Space down on a selected image entry until the key repeats
- **THEN** the preview opens once and stays open until Space is pressed again

### Requirement: Image preview content
An image preview SHALL show the entry's staged image whole, scaled down to fit the dialog and never scaled up beyond its own size, with its original name, pixel dimensions, and size.

#### Scenario: Large screenshot
- **WHEN** the user previews a 3840 × 2160 image entry of 2.1 MB
- **THEN** the dialog shows the whole image scaled to fit, and "3840 × 2160" and "2.1 MB"

#### Scenario: Small image
- **WHEN** the user previews a 64 × 64 image entry
- **THEN** the image is shown at 64 × 64, not enlarged

### Requirement: Text preview content
A text preview SHALL show the entry's staged text in a scrollable, read-only area, with its original name and size, where the user can select and copy text. Text longer than 1 MiB SHALL show only its first 1 MiB, cut at a character boundary, with a note saying that only the beginning is shown.

#### Scenario: Select and copy
- **WHEN** the user selects a line in a text preview and presses Ctrl+C
- **THEN** the clipboard holds that line, and the change is not captured as a new item

#### Scenario: Keyboard-only copy
- **WHEN** the user presses Tab to move the focus into a text preview's text, then Ctrl+A and Ctrl+C
- **THEN** the clipboard holds the text the preview shows

#### Scenario: Long text
- **WHEN** the user previews a text entry of 5 MB
- **THEN** the dialog shows its first 1 MB and a note that only the beginning is shown, and the text does not end in a broken character

#### Scenario: Text is not edited
- **WHEN** the user types in a text preview
- **THEN** the text and its staged file stay unchanged

### Requirement: Preview availability
The preview SHALL say when an entry's content can't be shown instead of failing silently: a file entry shows that files have no preview, and a text or image entry whose staged file is missing or unreadable shows that its content is no longer available. Loading SHALL NOT block the window; until the content is ready, the dialog shows that it is loading.

#### Scenario: Pruned entry
- **WHEN** the user previews a text entry whose staged file was deleted by the retention limits
- **THEN** the dialog says the content is no longer available, and the entry's other actions are unaffected

#### Scenario: Skipped because too large
- **WHEN** the user previews an image entry skipped because it exceeded the size limit, which was never staged
- **THEN** the dialog says the content is no longer available

#### Scenario: Window stays responsive
- **WHEN** the user previews a 40 MB screenshot
- **THEN** the dialog opens at once with a loading indicator, and the window keeps responding to input until the image appears

### Requirement: Browse while previewing
While the preview is open, Up and Down SHALL select the previous and next row of the current page, and the preview SHALL follow the selection, also while the focus is in a text preview's text. A result that arrives after the user moved to another row SHALL be discarded.

#### Scenario: Next entry
- **WHEN** the preview of the first row is open and the user presses Down
- **THEN** the second row is selected in the table, and the dialog shows the second row's content

#### Scenario: Browse onto a file entry
- **WHEN** the preview is open and Down selects a file entry
- **THEN** the dialog stays open and says that files have no preview

#### Scenario: Edge of the page
- **WHEN** the preview of the last row on the page is open and the user presses Down
- **THEN** the selection and the preview stay on that row

#### Scenario: Fast browsing
- **WHEN** the user presses Down three times quickly while the images load
- **THEN** the dialog ends up showing the third row below the starting one, never an earlier row's image

### Requirement: Close the preview
Space, Escape, the dialog's Close button, and a click outside the dialog SHALL close the preview. Closing SHALL return the keyboard focus to the history table with the previewed row still selected.

#### Scenario: Space closes
- **WHEN** a preview is open and the user presses Space
- **THEN** the dialog closes, the table has the focus, and the previewed row is selected

#### Scenario: Escape closes
- **WHEN** a preview is open and the user presses Escape
- **THEN** the dialog closes and the table has the focus

#### Scenario: Entry leaves the page
- **WHEN** a preview is open and the previewed entry is deleted, or new captures push it off the current page
- **THEN** the dialog closes, and the table has the focus

#### Scenario: New capture while previewing
- **WHEN** a preview is open and a new entry is captured while the previewed entry stays on the page
- **THEN** the dialog keeps showing the previewed entry

## MODIFIED Requirements

### Requirement: Entry actions
For each entry, the system SHALL offer the actions that apply. Preview (text and image entries). Copy remote path (uploaded). Retry (failed or cancelled). Upload anyway (skipped), which bypasses rules like a manual upload. Open local file and Show in Explorer (when the staged or original file still exists). Delete.

#### Scenario: Retry failed entry
- **WHEN** the user selects Retry on a failed entry while its profile is reachable
- **THEN** the entry is queued again to its original profile, and on success its status becomes uploaded

#### Scenario: Copy remote path
- **WHEN** the user selects Copy remote path on an uploaded entry
- **THEN** the clipboard contains that entry's absolute remote path, and the change is not captured as a new item

#### Scenario: Upload anyway
- **WHEN** the user selects Upload anyway on an entry skipped by "Skip large files"
- **THEN** the entry is queued with the active profile's defaults, and history records the trigger as manual

#### Scenario: Preview from the menu
- **WHEN** the user right-clicks a text entry and selects Preview
- **THEN** the preview dialog opens for that entry, as Space would open it

#### Scenario: No Preview for files
- **WHEN** the user right-clicks a file entry
- **THEN** the menu has no Preview item
