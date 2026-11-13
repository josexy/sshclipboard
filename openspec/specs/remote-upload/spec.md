# remote-upload Specification

## Purpose
Transfers each entry approved for upload to the active Linux server reliably and safely, then optionally puts the remote path back on the local clipboard so it can be pasted into the SSH session.

## Requirements

### Requirement: Background upload queue
The system SHALL queue each approved entry for the profile that was active when it was captured. Entries upload in the background in capture order, with at most two transfers at a time per profile. Queued uploads SHALL NOT delay the detection or capture of new clipboard changes.

#### Scenario: Burst of captures
- **WHEN** the user captures five screenshots within ten seconds while the network is slow
- **THEN** all five are queued, at most two transfer at once, and each finishes in history as uploaded or failed

### Requirement: Remote path resolution
The system SHALL resolve the target directory of each entry on the server: a leading `~/` and a relative path resolve against the login user's home directory, and an absolute path is used as is. History SHALL record the absolute remote path of every uploaded entry.

#### Scenario: Home-relative directory
- **WHEN** the directory is `~/clipboard`, the user is `alice`, and the server reports home `/home/alice`
- **THEN** the entry is uploaded to `/home/alice/clipboard/<filename>`, and history shows that absolute path

### Requirement: Create missing directories
When "create missing directories" is on (the default, set per profile), the system SHALL create every missing directory in the target path before uploading. When it is off, an upload to a missing directory SHALL fail with "remote directory does not exist".

#### Scenario: Dated folder created
- **WHEN** the target is `/home/alice/clipboard/2026-11` and that folder does not exist
- **THEN** the folder is created and the entry is uploaded into it

### Requirement: Atomic remote writes
The system SHALL write each entry to a temporary hidden file in the target directory and rename it to the final name only after all bytes are written. Watchers on the server SHALL never see a partial file under the final name, and a failed or cancelled transfer SHALL remove its temporary file when the connection allows.

#### Scenario: Connection lost mid-transfer
- **WHEN** the connection drops after 40% of a 50 MB file is sent
- **THEN** no file with the final name exists on the server, and the next attempt starts the transfer again

### Requirement: Name conflict policy
Each profile SHALL have a conflict policy for an existing file with the final name: rename (the default), overwrite, or skip. Rename appends `-1`, `-2`, and so on before the extension until the name is free. Skip records the entry as skipped with reason "already exists".

#### Scenario: Rename on conflict
- **WHEN** `report.pdf` and `report-1.pdf` already exist in the target directory and the policy is rename
- **THEN** the entry is uploaded as `report-2.pdf`

#### Scenario: Overwrite
- **WHEN** `notes.txt` exists and the policy is overwrite
- **THEN** the existing file is replaced with the new content

### Requirement: Remote file permissions
When a profile sets an optional octal file mode, such as `0644`, the system SHALL apply it to each uploaded file. When no mode is set, the server's default applies.

#### Scenario: Mode applied
- **WHEN** a profile's file mode is `0640` and an entry is uploaded
- **THEN** the remote file has mode `0640`

### Requirement: Retry transient failures
The system SHALL retry an entry after a transient failure (network error, timeout, or dropped connection) up to 3 times, waiting about 1, 2, and 4 seconds, and reconnect before each retry. Authentication failures, host key problems, permission denied, a missing source file, and a missing directory when creation is off SHALL fail immediately without retrying.

#### Scenario: Brief network blip
- **WHEN** the first attempt times out and the second attempt succeeds
- **THEN** the entry is recorded as uploaded with 2 attempts

#### Scenario: Permission denied
- **WHEN** the server returns permission denied when the file is created
- **THEN** the entry fails at once with the server's message and is not retried

### Requirement: Cancel uploads
The system SHALL let the user cancel any queued or in-progress entry. A cancelled entry stops transferring, its temporary remote file is removed, and it is recorded as cancelled.

#### Scenario: Cancel a large transfer
- **WHEN** the user cancels an entry that is 30% uploaded
- **THEN** the transfer stops within one second, and history shows the entry as cancelled with no file left on the server

### Requirement: Transfer progress
For entries larger than 1 MB, the system SHALL show the percentage transferred and the current speed while the transfer runs.

#### Scenario: Progress shown
- **WHEN** a 20 MB file is uploading
- **THEN** its row in the Activity view shows a percentage and a speed that update at least once per second

### Requirement: Source files read at upload time
For files entries, the system SHALL read the file from its original local path when the transfer starts. If the file no longer exists or cannot be read, the entry SHALL fail with "source file not found" or the read error.

#### Scenario: File deleted before upload
- **WHEN** the user copies `temp.log` and deletes it before its queued upload starts
- **THEN** the entry fails with "source file not found"

### Requirement: Interrupted uploads after restart
On startup, the system SHALL record entries that were queued or uploading when the app last stopped as failed with reason "interrupted", so the user can retry them.

#### Scenario: Quit during upload
- **WHEN** the app quits while two entries are queued and then restarts
- **THEN** history shows both entries as failed with "interrupted" and a Retry action

### Requirement: Replace the clipboard after upload
When the profile setting is on (the default), the system SHALL replace the clipboard with text built from the uploaded remote paths once every entry of a capture is resolved, provided at least one uploaded. The text uses a per-path template (default `{path}`) and a separator (space by default, newline as the other option).

#### Scenario: Screenshot path copied back
- **WHEN** a screenshot uploads to `/home/alice/clipboard/screenshot_20261112-153045.png`
- **THEN** the clipboard contains exactly `/home/alice/clipboard/screenshot_20261112-153045.png`

#### Scenario: Multiple files
- **WHEN** a files item with `a.txt` and `b.txt` uploads to `/srv/in`, the separator is space, and the template is `{path}`
- **THEN** the clipboard contains `/srv/in/a.txt /srv/in/b.txt`

#### Scenario: Setting off
- **WHEN** the profile's "copy remote path after upload" setting is off
- **THEN** the clipboard keeps the content the user copied

### Requirement: Clipboard text template
The clipboard template SHALL support `{path}` (absolute remote path), `{dir}`, `{name}`, `{user}`, `{host}`, `{port}`, and `{profile}`. When "shell-quote paths" is on (the default), each `{path}`, `{dir}`, and `{name}` value that contains spaces or shell metacharacters SHALL be wrapped in single quotes, with embedded quotes escaped.

#### Scenario: Prefixed reference
- **WHEN** the template is `@{path}` and the file uploads to `/home/alice/clipboard/a.png`
- **THEN** the clipboard contains `@/home/alice/clipboard/a.png`

#### Scenario: Path with spaces
- **WHEN** `My Report.pdf` uploads to `/srv/in` with shell-quoting on and template `{path}`
- **THEN** the clipboard contains `'/srv/in/My Report.pdf'`

#### Scenario: scp-style reference
- **WHEN** the template is `{user}@{host}:{path}` for user `alice`, host `10.0.0.5`, and path `/srv/in/a.txt`
- **THEN** the clipboard contains `alice@10.0.0.5:/srv/in/a.txt`

### Requirement: Don't overwrite newer clipboard content
The system SHALL NOT replace the clipboard after upload if the clipboard changed after the capture was taken. History still records the remote paths.

#### Scenario: User copied something else meanwhile
- **WHEN** a large file is uploading and the user copies other text before it finishes
- **THEN** the clipboard keeps the user's newer text after the upload finishes
