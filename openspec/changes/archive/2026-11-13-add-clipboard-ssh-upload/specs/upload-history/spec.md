# Spec Delta

## Purpose

Gives the user a durable, searchable record of what was captured, what was uploaded or skipped and why, and lets them act on past entries, while keeping local storage bounded.

## ADDED Requirements

### Requirement: Record every entry
The system SHALL record a history entry for every captured entry, including skipped ones. It records capture time, trigger (automatic or manual), kind, original name or local path, size, profile, the deciding rule (rule name, "default action", or "manual"), status, absolute remote path once known, attempt count, and the last error.

#### Scenario: Skipped entry recorded
- **WHEN** a text entry is skipped by the rule "Skip text"
- **THEN** history has an entry with kind text, status skipped, and rule "Skip text"

#### Scenario: Failure recorded
- **WHEN** an entry fails after 4 attempts with "connection refused"
- **THEN** its history entry has status failed, attempts 4, and error "connection refused"

### Requirement: Status lifecycle
An entry's status SHALL be one of queued, uploading, uploaded, skipped, failed, or cancelled. The Activity view SHALL reflect status changes while it is open, within one second.

#### Scenario: Live update
- **WHEN** the Activity view is open and a queued entry starts uploading and then finishes
- **THEN** its row changes from queued to uploading to uploaded without user action

### Requirement: Browse and filter history
The Activity view SHALL list history newest first, show image thumbnails for image entries, filter by status and kind, search by original name or remote path (case-insensitive substring), and offer a toggle to hide skipped entries.

#### Scenario: Search by remote path
- **WHEN** the user searches for `2026-11` in a history containing entries uploaded under `/home/alice/clipboard/2026-11/`
- **THEN** only those entries are listed

#### Scenario: Hide skipped
- **WHEN** the user turns on "Hide skipped"
- **THEN** entries with status skipped are not listed, and turning it off shows them again

### Requirement: Entry actions
For each entry, the system SHALL offer the actions that apply. Copy remote path (uploaded). Retry (failed or cancelled). Upload anyway (skipped), which bypasses rules like a manual upload. Open local file and Show in Explorer (when the staged or original file still exists). Delete.

#### Scenario: Retry failed entry
- **WHEN** the user selects Retry on a failed entry while its profile is reachable
- **THEN** the entry is queued again to its original profile, and on success its status becomes uploaded

#### Scenario: Copy remote path
- **WHEN** the user selects Copy remote path on an uploaded entry
- **THEN** the clipboard contains that entry's absolute remote path, and the change is not captured as a new item

#### Scenario: Upload anyway
- **WHEN** the user selects Upload anyway on an entry skipped by "Skip large files"
- **THEN** the entry is queued with the active profile's defaults, and history records the trigger as manual

### Requirement: Clear history
The system SHALL let the user delete single entries and clear all history after confirmation. Deleting an entry SHALL also delete its staged local file. Remote files SHALL never be deleted by history actions.

#### Scenario: Clear all
- **WHEN** the user selects Clear history and confirms
- **THEN** all entries that are not queued or uploading are removed with their staged files, and files on the server are unchanged

### Requirement: Retention limits
The system SHALL keep at most a configured number of history entries (default 1000) and remove entries older than a configured number of days (default 30). Removed entries' staged files are deleted with them. Pruning runs at startup and after each new entry, and never removes entries that are queued or uploading.

#### Scenario: Count limit
- **WHEN** history holds 1000 entries and a new entry is recorded with the default limit
- **THEN** the oldest finished entry and its staged file are removed

#### Scenario: Orphaned staged files
- **WHEN** the staging folder contains files that no history entry refers to at startup
- **THEN** those files are deleted

### Requirement: Today's summary
The Activity view SHALL show the number of entries uploaded today, the total bytes uploaded today, and the number of failures today.

#### Scenario: Summary counts
- **WHEN** three entries totalling 4.2 MB uploaded today and one failed
- **THEN** the summary shows 3 uploaded, 4.2 MB, and 1 failed
