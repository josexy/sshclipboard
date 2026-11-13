# Spec Delta

## Purpose

Turns changes to the local Windows clipboard into captured items (images, files, or text) that the rules and upload pipeline can act on, without capturing the app's own writes or confidential content.

## ADDED Requirements

### Requirement: Detect clipboard changes
While monitoring is on, the system SHALL detect every change to the system clipboard made by any application, including screenshot tools, file explorers, and editors, within one second of the change.

#### Scenario: Screenshot detected
- **WHEN** monitoring is on and the user takes a region screenshot with Win+Shift+S
- **THEN** the app captures one image item within one second

#### Scenario: Monitoring paused
- **WHEN** monitoring is paused and the user copies a file
- **THEN** no item is captured, and resuming monitoring does not capture that earlier copy

### Requirement: Debounce bursts
The system SHALL treat clipboard changes that happen within 300 ms of each other as one change and capture the clipboard's contents once the burst settles.

#### Scenario: Application writes several formats
- **WHEN** an application updates the clipboard three times within 100 ms while copying one image
- **THEN** exactly one image item is captured

### Requirement: Classify clipboard content
The system SHALL classify each change into exactly one kind, in priority order: files (a list of local paths), image, then text. Content with none of these, or only empty or whitespace-only text, SHALL be ignored.

#### Scenario: Files copied in Explorer
- **WHEN** the user copies `report.pdf` and `chart.png` in File Explorer
- **THEN** one files item with both paths is captured, and the image content of `chart.png` is not captured separately

#### Scenario: Image copied from a browser
- **WHEN** the user copies an image in a web browser, which also places HTML and text on the clipboard
- **THEN** one image item is captured

#### Scenario: Plain text
- **WHEN** the user copies the text "hello world" from an editor
- **THEN** one text item with that text is captured

### Requirement: Folders in a file list
The system SHALL leave directories out of a files item and record each one as skipped with reason "folders are not supported". If only directories were copied, the item SHALL be recorded as skipped.

#### Scenario: Mixed files and folder
- **WHEN** the user copies `notes.txt` and the folder `assets` in File Explorer
- **THEN** the files item contains only `notes.txt`, and history shows `assets` skipped because folders are not supported

### Requirement: Stage captured content locally
The system SHALL write image and text items to a local staging folder before upload: images as PNG and text as UTF-8 `.txt`. Files items SHALL refer to the original paths and not be copied. Each item SHALL record its kind, size in bytes, capture time, and content hash; images also record pixel width and height.

#### Scenario: Image staged
- **WHEN** an image item of 1280×720 pixels is captured
- **THEN** a PNG file exists in the staging folder, and the item records width 1280, height 720, its byte size, and a SHA-256 hash

### Requirement: Ignore the app's own clipboard writes
The system SHALL NOT capture clipboard changes that the app made itself, such as replacing the clipboard with a remote path after upload.

#### Scenario: Remote path written back
- **WHEN** an upload finishes and the app replaces the clipboard with `/home/alice/clipboard/shot.png`
- **THEN** no text item is captured for that change

### Requirement: Skip confidential content
By default, the system SHALL NOT capture clipboard content that its source marks as confidential or excluded from clipboard history and monitoring, as password managers do. A setting SHALL allow turning this protection off.

#### Scenario: Password manager copy
- **WHEN** a password manager copies a password and marks it excluded from clipboard monitoring
- **THEN** nothing is captured or recorded in history

### Requirement: Suppress duplicates
The system SHALL NOT capture an item whose kind and content hash match the previous captured item within 5 seconds. This prevents one action from being uploaded twice when an application rewrites identical content.

#### Scenario: Same screenshot rewritten
- **WHEN** a screenshot tool writes the same image to the clipboard twice, 1 second apart
- **THEN** only one image item is captured

#### Scenario: Same content later
- **WHEN** the user copies the same text again 10 seconds later
- **THEN** a new text item is captured

### Requirement: Capture modes
The system SHALL support two capture modes. In automatic mode, every detected change is captured. In manual mode, content is captured only when the user triggers "Upload clipboard now" from the global hotkey, the tray menu, or the main window.

#### Scenario: Manual mode ignores changes
- **WHEN** capture mode is manual and the user copies a screenshot
- **THEN** nothing is captured until the user presses the configured hotkey, which then captures the screenshot

### Requirement: Manual upload bypasses rules
A capture triggered by "Upload clipboard now" SHALL bypass the capture rules and upload with the active profile's default directory and the default naming templates. It works in both modes, including for content that a rule skipped before.

#### Scenario: Upload content a rule skipped
- **WHEN** a rule skipped a text item and the user then triggers "Upload clipboard now" with the same text on the clipboard
- **THEN** the text is uploaded, and history records the trigger as manual

### Requirement: Size limit for in-memory content
The system SHALL refuse image or text content whose clipboard representation is larger than 64 MB. Such a capture is recorded as skipped with reason "content too large", and nothing is written to the staging folder. Files items have no such limit.

#### Scenario: Huge bitmap
- **WHEN** the clipboard holds a 7680×4320 screenshot whose bitmap is about 132 MB
- **THEN** history records it as skipped with "content too large", and no staged file is created

#### Scenario: Large file is not limited
- **WHEN** the user copies a 500 MB file in File Explorer
- **THEN** a files item is captured, and the capture rules decide whether it uploads
