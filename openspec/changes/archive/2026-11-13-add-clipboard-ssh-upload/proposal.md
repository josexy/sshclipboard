# Proposal

## Why

On a remote Linux server reached over SSH, a terminal can't receive a local screenshot or a copied file. You have to save it, open an SFTP client, find the folder, upload, and type the path. This hurts most when a tool on the server needs a file path, such as a CLI agent that takes an image path. SSHClipboard removes those steps on Windows. It watches the local clipboard, sends matching screenshots, files, and text to a configured folder on the active server, and puts the remote path back on the clipboard so it can be pasted straight into the SSH session.

The repository is currently the MyGo counter template (`main.go`, MyGo v0.9.0, native `ui` package). This change turns it into the first version of the product.

## What Changes

- **Server profiles**: named SSH targets (host, port, user, auth by password, private key, or Windows OpenSSH agent) with a default remote folder. You can test the connection, and host keys are verified on first use. One profile is active at a time and can be switched from the window or the tray. Passwords and key passphrases are kept in the Windows Credential Manager, never in the database.
- **Clipboard capture**: Windows clipboard monitoring that turns each change into a captured item: an image (saved as PNG), a list of local files, or text. It debounces bursts of changes, ignores the app's own clipboard writes and content that password managers mark as confidential, and can be paused. Two modes: automatic (every change) or manual (global hotkey / tray action only).
- **Capture rules**: an ordered list of user rules that decides whether a captured item is uploaded or skipped. Rules can match on content kind, file extension, filename glob, size range, image dimensions, and text regex. A rule can override the destination folder and the filename template. Defaults are seeded on first run: text is skipped, and files over 100 MB are skipped.
- **File naming**: filename templates for each content kind, with tokens for the original name, extension, date/time (custom formats), counter, random string, content hash, profile, and host. The UI shows a live preview. Output is sanitized for Linux filesystems and shells.
- **Remote upload**: SFTP upload through a queue. It creates missing folders, writes to a temporary name and then renames atomically, handles name conflicts (rename / overwrite / skip), retries, reconnects, and can be cancelled. Captured items are first written to a local staging folder, so failed uploads can be retried. After a successful upload, the clipboard can be replaced with the remote path(s) using a template (per-profile setting, on by default).
- **Upload history**: a searchable list of captured, skipped, uploaded, and failed items, with copy-path, retry, open-local-file, and delete actions, plus retention limits for history rows and staged files.
- **Configuration storage**: a SQLite database (MyGo `plugins/sqlite`, no cgo) in the user data folder, with versioned schema migrations. It stores settings, profiles, rules, known hosts, and history.
- **Desktop shell**: a native MyGo UI with a sidebar (Activity, Servers, Rules, Naming, Settings) and a light/dark/system theme that uses a warm-neutral palette with a terracotta accent, similar to Claude's interface. It adds a tray icon with status and quick actions, close-to-tray, single instance, start-at-login, and Windows notifications. All user-facing text goes through MyGo i18n catalogs; English is the only language for now.

## Capabilities

### New Capabilities

- `ssh-profiles`: server profiles, their authentication and secret storage, host-key trust, connection testing, and choosing the active profile.
- `clipboard-capture`: watching the clipboard, classifying content into captured items, staging them locally, suppressing duplicates and self-writes, and automatic vs. manual capture modes.
- `capture-rules`: the ordered rule list, its match conditions, upload/skip actions with destination and naming overrides, the default action, and the seeded defaults.
- `file-naming`: the filename template language, per-kind templates, sanitization, and previewing.
- `remote-upload`: the SFTP transfer pipeline (queue, directory creation, atomic writes, conflict policy, retries, reconnection, cancellation) and replacing the clipboard after upload.
- `upload-history`: recording each item's outcome, the history view's actions, and retention of history rows and staged files.
- `config-storage`: SQLite persistence of all configuration and history, schema migrations, defaults on first run, and recovery from an unreadable database.
- `desktop-shell`: the main window's navigation, theming (system/light/dark, warm-neutral palette), tray icon and menu, close-to-tray, single instance, start at login, notifications, global hotkey, and i18n with English only.

### Modified Capabilities

None. The project has no existing specs.

## Impact

- **Code**: `main.go` and `main_test.go` (template counter) are replaced. New packages go under `internal/` (storage, secrets, clipboard watcher, capture, rules, naming, SSH/SFTP, uploader, history, UI views, theme) and a `locales/` catalog package. `mygo.json` gains `i18n` configuration.
- **Dependencies**: `golang.org/x/crypto/ssh` (+ `knownhosts`, `agent`), `github.com/pkg/sftp`, `github.com/Microsoft/go-winio` (named pipe to the Windows OpenSSH agent), `github.com/egoist/mygo/plugins/sqlite` (ships a native SQLite library alongside the app). Everything stays buildable with `CGO_ENABLED=0`.
- **Platform**: Windows is the only supported target. Win32 calls (clipboard sequence number) sit behind build tags. The app must still build, and its unit and UI tests must still run, on Linux, where development happens.
- **Security**: SSH credentials are kept in the OS credential store. Unknown or changed host keys require explicit user approval. Uploaded content can contain sensitive data, so confidential clipboard content is skipped by default.
- **Out of scope for this change**: macOS/Linux as runtime targets, folder (directory) uploads, importing hosts from `~/.ssh/config`, jump hosts/proxies, matching rules on the source application, image format conversion, config import/export, and languages other than English.
