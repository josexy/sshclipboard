# Tasks

## 1. Project scaffolding and dependencies

- [x] 1.1 Create the package layout from design D1 (`internal/{store,secret,clipwatch,capture,rules,naming,sshconn,uploader,core,view}`, `locales/`) with package doc comments, and verify `CGO_ENABLED=0 go build ./...` succeeds
- [x] 1.2 Add `golang.org/x/crypto` (ssh, ssh/agent, ssh/knownhosts), `github.com/pkg/sftp`, `github.com/Microsoft/go-winio`, and `github.com/egoist/mygo/plugins/sqlite` to `go.mod`, and verify `go mod tidy && CGO_ENABLED=0 go build ./... && GOOS=windows go build ./...` succeed
- [x] 1.3 Add `"i18n": {"dir": "locales", "source": "en"}` to `mygo.json`, create `locales/en.json` with the app name and window title, run `go tool mygo generate`, and verify `locales/catalog_gen.go` is generated and compiles
- [x] 1.4 Replace the template counter in `main.go`/`main_test.go` with a minimal window showing the localized title, and verify `CGO_ENABLED=0 go test ./...` passes with the updated `ui.NewTester` test

## 2. Configuration storage (`internal/store`)

- [x] 2.1 Implement opening `sshclipboard.db` under `mygo.PathUserData` with WAL and `foreign_keys`, plus a test helper that opens a temp-dir DB and skips with a clear message when the SQLite native library is unavailable, and verify the helper test runs
- [x] 2.2 Implement the embedded migration runner (statements split per file, each migration plus `PRAGMA user_version` in one `Transaction`), and verify tests for an ordered upgrade from v0, rollback of a failing migration leaving the version unchanged, and refusal to open a newer-version DB
- [x] 2.3 Write migration `0001` with the v1 schema from design D5 (settings, profiles, rules, naming, known_hosts, counters, history with indexes and FK actions), and verify a schema test that inspects `sqlite_master` and the CHECK constraints
- [x] 2.4 Implement the `quick_check` corruption recovery (rename to `.corrupt-<ts>`, recreate, report the path), and verify a test that writes random bytes to the DB file
- [x] 2.5 Implement first-run defaults (settings values, naming templates per kind, "Skip text" and "Skip large files" rules, default action upload), and verify a test that a fresh DB contains exactly these defaults
- [x] 2.6 Implement typed repositories for settings, profiles (CRUD plus unique-name error mapping), rules (ordered list, reorder, CRUD), naming, known hosts, counters (`next(profile, day)` returning the incremented value), and history (insert, status transitions, paged query with filters and search, today's summary, prune by count and age that skips queued/uploading rows, mark interrupted), and verify a unit test for each repository covering its spec scenarios
- [x] 2.7 Add a test that saves profiles with passwords through the full save path and greps every TEXT/BLOB column for the secret values, and verify it passes (spec *Secrets excluded*)

## 3. Secret storage (`internal/secret`)

- [x] 3.1 Implement `secret.Store` over `mygo.Secrets` with keys `profile/<id>/password|passphrase`, an in-memory session fallback on store errors, and delete-on-profile-delete, and verify tests using an injectable backend that cover set, get, not-found, the unsupported-store fallback, and delete

## 4. File naming (`internal/naming`)

- [x] 4.1 Implement the template parser with all tokens, `{{`/`}}` escapes, `N` and `FORMAT` arguments, and positioned errors for unknown tokens and unbalanced braces, and verify table tests including `{nmae}` and `{date:QQ}` rejections
- [x] 4.2 Implement the date `FORMAT` translation (YYYY, YY, MM, DD, HH, mm, ss, SSS, literals), and verify table tests including `YYYY/MM/DD_HH-mm-ss.SSS` at 09:05:07.250
- [x] 4.3 Implement rendering with injectable clock, random source, and counter, the automatic `.ext` append when `{ext}` is absent, and the lowercase and spaces-to-underscore transforms, and verify tests for every file-naming spec scenario (default image, file, and text names, counter, hash and random, literal braces)
- [x] 4.4 Implement `Sanitize` (unsafe characters, trimming, leading dash, 255-byte UTF-8 limit keeping the extension, empty fallback) and `RenderDir` (per-segment sanitize, `..` rejection, `~/` preserved), and verify table tests including `-rf: a/b.txt` → `_rf_ a_b.txt`, a 300-byte name, and `~/clipboard/../../etc`
- [x] 4.5 Document the token reference in `docs/naming.md`, and verify every example in it is covered by a test case in `naming_test.go`

## 5. Capture rules (`internal/rules`)

- [x] 5.1 Implement `Compile` (regex RE2, case-insensitive glob, min/max validation, template validation via `naming.Parse`) returning per-rule errors, and verify tests that reject `([a-z`, min > max, and a bad template
- [x] 5.2 Implement `Evaluate(Entry)` with ordered first match, disabled-rule skip, AND of conditions, kind-inapplicable conditions not matching, image/text pseudo names (`image.png`, `text.txt`), and the default action, and verify table tests for every capture-rules spec scenario
- [x] 5.3 Implement directory override resolution (absolute, `~/`, relative to the profile directory) via `naming.RenderDir`, and verify the `screenshots/{date}` → `~/clipboard/screenshots/2026-11-12` test
- [x] 5.4 Document rule conditions and evaluation order in `docs/rules.md`, and verify the documented examples match test cases

## 6. SSH connections (`internal/sshconn`)

- [x] 6.1 Build a test harness: an in-process SSH server with an SFTP subsystem rooted in a temp dir on `127.0.0.1:0`, configurable host key, password/public-key auth, and fault injection (drop after N bytes, deny create), and verify a smoke test can connect and list the root
- [x] 6.2 Implement auth method builders (password plus keyboard-interactive, private key with passphrase handling, agent via the Windows named pipe with `$SSH_AUTH_SOCK` on other OSes) with clear errors for a missing or unparseable key and an unavailable agent, and verify harness tests for each method and each error
- [x] 6.3 Implement the non-blocking host-key callback (DB known_hosts, then read-only `~/.ssh/known_hosts`, typed `HostKeyError` for unknown or changed keys with SHA-256 fingerprints), and verify tests for trusted, unknown, changed, and OpenSSH-known keys
- [x] 6.4 Implement the per-profile pool (lazy dial, shared ssh and sftp clients, cached home dir, 30 s keepalive with two-miss teardown, redial on next use, closing inactive profiles when idle) with a status event stream, and verify harness tests that kill the server connection and observe reconnect on the next use
- [x] 6.5 Implement Test connection (dial, auth, resolve dir, create dirs, probe write/remove, first failing step reported), and verify harness tests for success with the resolved absolute path and for a non-writable directory failure

## 7. Upload pipeline (`internal/uploader`)

- [x] 7.1 Implement remote path resolution and directory handling (`~/` and relative against home, `MkdirAll` when enabled, "remote directory does not exist" otherwise), and verify harness tests for both settings
- [x] 7.2 Implement the atomic write (`.name.rand.part`, progress-counting reader throttled to 4 per second, optional chmod, cleanup on failure or cancel), and verify harness tests that no final-name file exists after an injected mid-transfer drop and that the mode is `0640` when set
- [x] 7.3 Implement the conflict policies (rename via a non-overwriting `Rename` loop `-1…-999`, overwrite via `PosixRename` with fallback, skip via a pre-transfer `Stat`), and verify harness tests for `report-2.pdf`, overwrite content, and an "already exists" skip
- [x] 7.4 Implement error classification and retry (3 retries with 1/2/4 s backoff plus jitter, redial before retry, permanent errors fail at once), and verify tests with an injected timeout that then succeeds (2 attempts) and with permission denied (1 attempt)
- [x] 7.5 Implement per-profile FIFO queues with 2 workers, per-job cancel, parking on `HostKeyError` until resumed, and job events (started, progress, done, failed, cancelled), and verify tests for ordering, the concurrency cap of 2, cancel within 1 s with the `.part` removed, and park/resume

## 8. Clipboard watching and capture (`internal/clipwatch`, `internal/capture`)

- [x] 8.1 Implement the `Watcher` interface, the Windows watcher (150 ms `GetClipboardSequenceNumber` poll, `GetClipboardOwner` PID check, sequence accessor), and the portable fallback watcher, and verify `GOOS=windows go vet ./internal/clipwatch` plus a unit test of the fallback with a fake clipboard
- [x] 8.2 Implement debounce (300 ms quiet), classification priority (files > image > text, whitespace-only text ignored), folder filtering with skipped records, the confidential-format check with its setting, and self-write suppression, and verify tests with a fake clipboard source and fake watcher for each clipboard-capture scenario
- [x] 8.3 Implement staging (PNG/TXT under `<staging>/<yyyy-mm>/`, SHA-256 while writing, image width/height, 96 px thumbnail, 64 MB "content too large" skip) and the 5 s kind+hash dedupe, and verify tests for the 1280×720 staging record, oversize skip, and the 1 s vs 10 s duplicate scenarios
- [x] 8.4 Implement automatic vs manual modes and `CaptureNow()` (bypasses rules, trigger manual) with pause/resume that does not capture pre-resume content, and verify tests for each mode and for pause/resume

## 9. Core controller (`internal/core`)

- [x] 9.1 Implement the Controller state, the event loop, and the `notify` hook (injected; `win.Update` in production), wiring capture → rules → naming (counter reserved in the same transaction as the history insert) → history (queued/skipped) → uploader, and verify an integration test with a fake clipboard, a temp DB, and the SSH harness that a captured image ends as uploaded with the expected remote path
- [x] 9.2 Implement active-profile switching (persisted; in-flight jobs keep their profile) and delete-active-profile behavior (secrets, known host, and active cleared), and verify tests for the switch-during-upload and restart-keeps-active scenarios
- [x] 9.3 Implement copy-back per capture (all entries resolved, at least one uploaded, per-path template with `{path} {dir} {name} {user} {host} {port} {profile}`, separator, POSIX single-quote escaping, skipped when the clipboard changed since capture), and verify tests for every remote-upload copy-back scenario, including `@{path}`, scp style, a path with spaces, and the newer-clipboard guard
- [x] 9.4 Implement history actions (retry with the stored name, upload anyway as manual, copy path without capture, delete with staged file, clear all) and retention (startup and after each insert, orphan staged-file sweep), and verify tests for each upload-history scenario
- [x] 9.5 Implement startup recovery (mark queued/uploading as interrupted) and the host-key verification flow (pending prompt, trust or replace, resume the parked queue), and verify tests for the quit-during-upload and first-connection-hidden-window scenarios
- [x] 9.6 Implement the notification policy (All / Errors only / Off) and the tray-state derivation (monitoring, paused, uploading, error cleared on success or when Activity is viewed) as pure functions, and verify table tests

## 10. Theme and i18n foundations (`internal/view`, `locales`)

- [x] 10.1 Implement `view/theme.go` with the light and dark palettes from design D11 (radius, spacing, inactive accent, inverse, serif heading helper), and verify a test that asserts WCAG contrast of at least 4.5:1 for Text and TextMuted on Background and Sidebar in both themes
- [x] 10.2 Wire the Appearance setting to `mygo.Theme.SetSource` before `App.Run` and on change, with the view choosing its palette from `c.Theme().Dark`, and verify a `ui.NewTester` test that toggling Dark changes the rendered background color
- [x] 10.3 Add all UI, tray, and notification strings to `locales/en.json` (ICU plurals for counts), call `mygo.App.SetLanguages("en")`, and verify `go tool mygo generate` is clean and a test that `"1 file uploaded"` and `"2 files uploaded"` render correctly

## 11. Main window views (`internal/view`)

- [x] 11.1 Build the shell (sidebar with five pages, header with active-server select, status dot, monitoring switch, and "Upload clipboard now"), and verify a `ui.NewTester` test that navigates every page and toggles monitoring through the Controller
- [x] 11.2 Build the Activity page (summary strip, search, status/kind filters, hide-skipped, virtualized table with thumbnail, status, remote path, and progress, plus row actions), and verify tester tests for search, hide skipped, live status updates via `notify`, and each row action calling the Controller
- [x] 11.3 Build the Servers page (list, form with validation, auth method switch, key file picker, secret fields that never prefill, Test connection result panel, duplicate/delete with confirm, host-key trust and replace dialog), and verify tester tests for the create-with-defaults, invalid-fields, and trust-dialog flows
- [x] 11.4 Build the Rules page (ordered list with drag and Move up/down, enable toggle, edit dialog with all conditions and overrides, default action, "Test with current clipboard" results panel), and verify tester tests for the invalid-regex error, reorder, and dry-run output
- [x] 11.5 Build the Naming page (three templates, live preview with the active profile, token help popover, transforms, inline errors), and verify tester tests for the `{nmae}` error and the `dev-box_example.txt` preview
- [x] 11.6 Build the Settings page (General: start at login, start hidden, close to tray, capture mode, hotkey, confidential protection; Appearance; Notifications; Storage: staging folder picker, retention; About: version), and verify tester tests that each control persists through the store
- [x] 11.7 Add a keyboard-traversal tester test for the Servers and Settings pages (Tab/Shift+Tab reach every control in order, Enter/Space activate), and verify it passes and that `go tool mygo vet .` reports no stored elements or goroutine captures

## 12. Desktop integration (`main.go`, `internal/core`)

- [ ] 12.1 Add the single-instance lock and `OnSecondInstance` show/focus, `StateKey` window geometry, `Hidden: WasOpenedAtLogin()`, and close-to-tray with the one-time notification and quit confirmation while uploads are in flight, and verify with a Windows build plus the manual checklist items in 13.2
- [ ] 12.2 Implement the tray (state icons composed from `resources/icon.png`, tooltip, menu rebuilt on state change with Pause/Resume, Upload now, a Servers radio submenu, Open, and Quit) and verify a unit test of the menu model per state plus the manual checklist
- [ ] 12.3 Implement Windows notifications with a click that opens Activity, and the global hotkey (default Ctrl+Alt+U, register-new-before-unregister-old, error on conflict), and verify unit tests of the hotkey swap logic with a fake registrar plus the manual checklist
- [ ] 12.4 Implement the Start at login setting via `App.SetOpenAtLogin`, and verify the stored setting and `App.OpenAtLogin()` agree after toggling in a Windows smoke test

## 13. Integration verification and packaging

- [x] 13.1 Run the full gates `CGO_ENABLED=0 go test ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=windows GOARCH=arm64 go build ./...`, and `go tool mygo vet .`, and verify all pass
- [ ] 13.2 Write `docs/manual-test.md` (Win+Shift+S screenshot, Explorer multi-file copy with a folder, password-manager copy, text skipped by default, copy-back paste into an SSH terminal, pause/resume from the tray, hotkey from another app, host-key first use and change, server reboot reconnect, close-to-tray, second launch, login start, dark/light/system switch) and run it on a Windows 11 machine against a Linux server, recording results in the file
- [ ] 13.3 Build release bundles with `go tool mygo build` for windows/amd64 and windows/arm64, and verify the bundle contains the SQLite native library and the app starts on a clean Windows VM with a fresh database
- [x] 13.4 Write the user-facing `README.md` (what it does, install, first server setup, rules and naming links, privacy notes on clipboard uploading and credential storage), and verify every link and documented default matches the shipped behavior

## Workflow follow-up

- Archive the change with `/opsx:archive` after review and the manual Windows checklist pass.
- After archiving, verify `openspec/specs/` contains the eight new capability specs.
