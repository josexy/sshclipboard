# Design

## Context

- The repository is the MyGo v0.9.0 native-UI template: `main.go` (a counter in `ui.View`), `main_test.go` (`ui.NewTester`), `mygo.json`, and `resources/icon.png`. Module `github.com/josexy/sshclipboard`, Go 1.27.3. Nothing from the template is kept except the app skeleton.
- MyGo constraints that shape the design (checked against the pinned source):
  - **No cgo.** Everything builds with `CGO_ENABLED=0`. Native calls use `syscall` / purego.
  - **Threading.** Views run on the main thread. `ui.Element`s expire every build pass. Background work publishes through `Window.Update(fn)`, and `c.Services()` is the handle for redraws and clipboard access from outside a view.
  - **Clipboard.** `mygo.Clipboard.Read/ReadFiles/ReadImage/ReadText/Formats/WriteText` are safe from any goroutine because they marshal to the main thread. The Windows backend maps CF_DIB/registered PNG to `image/png` and CF_HDROP to a file list, caps each representation at **64 MiB**, and reports registered format names such as `ExcludeClipboardContentFromMonitorProcessing`. **MyGo has no clipboard-change notification**, so the app must detect changes itself.
  - Available modules: `mygo.Secrets` (Windows Credential Manager), `mygo.NewTray`, `mygo.NewNotification` and `App.OnNotificationClick`, `mygo.GlobalShortcut.Register`, `App.RequestSingleInstanceLock` and `OnSecondInstance`, `App.SetOpenAtLogin` and `WasOpenedAtLogin`, `mygo.Theme.SetSource(system|light|dark)`, `App.SetLanguages`, `mygo.Dialog.Open`, and `StateKey` for window geometry.
  - `plugins/sqlite`: SQLite built as a Zig native module, no cgo. Go API `sqlite.Open(ctx, path, opts)`, `Execute`/`Query`/`Transaction`. `mygo build` ships the library with the app, and `go run`/`go test` download it.
- Development happens on Linux arm64, but the only runtime target is Windows. The code must build on Linux and pass its unit and UI tests there.
- The user chose plain `mygo/ui` (not `nuxt-ui-go`). Clipboard copy-back is configurable and on by default.

## Goals / Non-Goals

**Goals:**
- A capture → rules → naming → upload → history pipeline that runs headless, with the UI only observing and commanding it. The pipeline works with the window hidden or closed to the tray.
- Pure, table-testable cores (`naming`, `rules`) and hermetic integration tests for SSH/SFTP using an in-process server.
- Every Windows-specific call behind build tags with a portable fallback, so `go test ./...` runs on Linux.

**Non-Goals:**
- A reusable component library or a design-system package. Theme and widgets stay app-local.
- Pageant (PuTTY agent), PuTTY `.ppk` keys, jump hosts, SOCKS/HTTP proxies, SSH certificates.
- Resumable or delta transfers. A failed transfer restarts from byte 0.
- Uploading through more than one profile at the same time for a single capture.

## Decisions

### D1. Package layout

```
main.go                       wiring: single-instance lock, open store, build services, window, tray
locales/en.json               message catalog (+ generated catalog_gen.go, committed)
resources/icon.png            existing app icon; tray state badges are composed from it at startup
internal/
  store/      SQLite access: migrations (embedded SQL), typed repos for settings, profiles,
              rules, naming, known hosts, counters, history
  secret/     credential store wrapper + in-memory session fallback
  clipwatch/  change detection: watcher_windows.go (Win32), watcher_other.go (portable polling)
  capture/    debounce, read + classify clipboard, stage to disk, hash, thumbnails, dedupe,
              self-write and confidential filtering
  rules/      pure rule compilation + first-match evaluation
  naming/     pure template parser/renderer, sanitizer, directory templates
  sshconn/    auth methods, host-key verification, per-profile connection pool, keepalive
  uploader/   per-profile queues and workers, SFTP put (temp + rename), conflicts, retry, cancel, progress
  core/       the Controller: owns services, runs the pipeline, exposes actions and a state snapshot,
              emits events; copy-back to clipboard; notifications; hotkey; tray state
  view/       mygo/ui views (shell, activity, servers, rules, naming, settings, dialogs) and theme.go
```

*Why:* each spec capability maps to one package (history lives in `store` + `core`), and the packages with real logic (`naming`, `rules`, `capture`, `uploader`) don't import `mygo` or `mygo/ui` directly. They take small interfaces (clipboard source, clock, filesystem), so they test without a window. The views package is named `view` so it doesn't clash with `mygo/ui`.
*Alternative:* one `app` package. It would be quicker to start, but it couples tests to the window and to Win32.

### D2. Clipboard change detection: poll `GetClipboardSequenceNumber`

On Windows, `clipwatch` polls `user32!GetClipboardSequenceNumber` every 150 ms on a plain goroutine. The function needs no window and no clipboard lock and costs one syscall. Any change of the number emits a tick. The **capture** layer debounces ticks (300 ms of quiet), then reads the clipboard through `mygo.Clipboard`.

*Alternative:* `AddClipboardFormatListener` on a message-only window (`HWND_MESSAGE`) with its own locked-OS-thread message loop. It's event-driven with zero idle cost, but it needs about 150 lines of Win32 window-class and message-pump code through `syscall`, and lifecycle care beside MyGo's own message loop. The polling cost is negligible and meets the "within 1 s" requirement. The `Watcher` interface (`Changes() <-chan uint32`, `Seq() uint32`, `Close()`) lets us swap this in later without touching `capture`.
*Fallback (non-Windows, dev only):* poll `mygo.Clipboard.Formats()` plus a SHA-256 of the text/PNG once a second and synthesize sequence numbers. It's only good enough to run the app on Linux while developing.

### D3. Self-writes, confidential content, and the "clipboard changed since" check

- **Self-writes:** on Windows, when a change arrives, `GetClipboardOwner` → `GetWindowThreadProcessId`. If the owner PID is our own, the change is ignored. This is definitive: MyGo writes through `OleSetClipboard`, whose owner window belongs to our process. Fallback (other OS / owner unavailable): the capture layer remembers the fingerprint of the last text the app wrote and ignores an identical text change within 2 s.
- **Confidential:** skip when `Formats()` contains `ExcludeClipboardContentFromMonitorProcessing`, `Clipboard Viewer Ignore`, or `CanIncludeInClipboardHistory` (the last one only when its DWORD is 0). These are the conventions KeePass, 1Password, and Bitwarden use on Windows. A setting turns the check off.
- **Copy-back guard:** each capture stores the sequence number it was taken at. Before copy-back, `core` compares it with the current `Seq()`. If they differ and the current owner isn't us, the user copied something newer, so copy-back is skipped (spec: *Don't overwrite newer clipboard content*).

### D4. Classification and staging

`capture` reads formats once and picks **files > image > text**. Files come from `ReadFiles()`, with directories filtered out and recorded as skipped. Images come from `ReadImage()` (PNG bytes; MyGo prefers the registered PNG format over DIB, which keeps alpha). Text comes from `ReadText()`. A read error from the 64 MiB cap is recorded as "content too large".

Images and text are written to `<staging>/<yyyy-mm>/<capture-id>.<ext>` with a 0600-equivalent ACL (inherits from the user profile folder). Images also get a 96 px-high PNG thumbnail `<capture-id>.thumb.png`, so the Activity view never decodes full screenshots. The SHA-256 hash is computed while writing. Each capture gets a ULID-like `capture_id` that ties its history rows together, so copy-back knows when all entries of a capture are resolved.

### D5. Storage: MyGo `plugins/sqlite`, one connection, migrations via `PRAGMA user_version`

- DB file `mygo.App.Path(mygo.PathUserData)/sshclipboard.db`. One `*sqlite.DB` is shared; the plugin serializes operations per connection, and the single-instance lock guarantees a single writer. `PRAGMA journal_mode=WAL` and `synchronous=NORMAL` are set on open.
- Migrations are embedded `migrations/NNNN_*.sql`. On start: `PRAGMA quick_check`. A failure renames the file to `.corrupt-<ts>` and starts fresh (spec *Recovery*). Then read `user_version`. If it's greater than the app's latest, refuse to open (spec *Database from a newer app*). Otherwise apply each pending file plus `PRAGMA user_version = N` inside one `db.Transaction`. The plugin rejects BEGIN/COMMIT in user SQL, so each migration is a list of single statements split at build time from the embedded file.
- Schema (v1):
  - `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`: JSON-encoded values. Typed accessors in `store.Settings`. A key-value table avoids a migration for every new toggle.
  - `profiles(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, host, port, username, auth_method CHECK(auth_method IN ('password','key','agent')), key_path, remote_dir, create_dirs, conflict_policy CHECK(... IN ('rename','overwrite','skip')), file_mode NULL, copy_back, copy_template, copy_separator, shell_quote, created_at, updated_at)`
  - `rules(id INTEGER PRIMARY KEY, position INTEGER NOT NULL, name, enabled, action CHECK(action IN ('upload','skip')), conditions TEXT /*JSON*/, dir_override NULL, template_override NULL)`: conditions are stored as JSON so new condition types need no migration.
  - `naming(kind TEXT PRIMARY KEY CHECK(kind IN ('image','file','text')), template, lowercase, spaces_to_underscore)`
  - `known_hosts(host, port, key_type, key_b64, fingerprint_sha256, added_at, PRIMARY KEY(host, port))`
  - `counters(profile_id, day TEXT, value, PRIMARY KEY(profile_id, day))`, with `ON DELETE CASCADE` from profiles.
  - `history(id INTEGER PRIMARY KEY, capture_id, captured_at, trigger, kind, original_name, local_path, staged, thumb_path, size, width, height, sha256, profile_id NULL REFERENCES profiles ON DELETE SET NULL, profile_name, rule_name, action, status, remote_path, attempts, error, finished_at)`, plus indexes on `captured_at` and `status`. `profile_name` is a snapshot so history stays readable after a profile is deleted.

*Alternative:* `modernc.org/sqlite` (pure Go, `database/sql`). It's self-contained and works in tests without a downloaded library, but adds about 6–8 MB to the binary and duplicates something the framework already ships. If the plugin's library download proves flaky in CI, `store` keeps all SQL in one package behind a narrow `Exec/Query/Tx` interface, so the driver can be swapped without touching callers.

### D6. Secrets

`secret.Store` wraps `mygo.Secrets` with keys `profile/<id>/password` and `profile/<id>/passphrase`. Profiles store no secret material, only `auth_method` and `key_path`. On `errors.Is(err, errors.ErrUnsupported)` or any store error, it falls back to an in-memory map for the session, and `core` prompts when a secret is missing (spec *Credential store unavailable*). Deleting a profile deletes its secrets and its `known_hosts` row.

### D7. SSH: `golang.org/x/crypto/ssh` + `github.com/pkg/sftp`, a connection pool keyed by profile

- Auth methods: password (`ssh.Password` + `KeyboardInteractive` answering every prompt with the password); key (`ssh.ParsePrivateKey`, and on `*ssh.PassphraseMissingError` `ParsePrivateKeyWithPassphrase`; OpenSSH and PEM formats); agent via `github.com/Microsoft/go-winio` `DialPipe(\\.\pipe\openssh-ssh-agent)` → `agent.NewClient`. On non-Windows the agent uses `$SSH_AUTH_SOCK`, so the dev build works.
- **Host keys:** a custom `HostKeyCallback` checks `known_hosts` in the DB first, then (read-only) `%USERPROFILE%\.ssh\known_hosts` through `knownhosts.New`, so hosts the user already trusts in OpenSSH aren't prompted again. On unknown or mismatched keys the callback **doesn't block**. It returns a typed `*HostKeyError{Host, Port, Key, Known}`, the handshake fails fast, and the uploader parks the profile's queue in a "needs verification" state. `core` then shows a trust dialog (window visible) or a notification (hidden). On Trust it saves the key and resumes the queue. *Why not block inside the callback:* waiting for a human would hold the TCP handshake open past server `LoginGraceTime` (120 s by default) and tie up a goroutine for each queued item.
- **Pool:** one `*ssh.Client` and one `*sftp.Client` per profile, created lazily and shared by that profile's workers (`sftp.UseConcurrentWrites(true)`). Keepalive is `SendRequest("keepalive@openssh.com", true, nil)` every 30 s while the profile is active. Two misses mark the connection dead and close it, and the next use redials. Pools for inactive profiles close once their queue is empty. The remote home directory is cached from `sftp.Getwd()` per connection.
- **Test connection** runs dial → auth → resolve dir → `MkdirAll` (when create_dirs is on) → create and remove a probe file `.sshclipboard-probe-<rand>`, reporting the first failing step.

### D8. Upload algorithm

For each entry (`uploader.Job{HistoryID, ProfileID, LocalPath|Reader, DirTemplateResult, FileName, Policy, Mode, Ctx}`):
1. Resolve the directory: `~/x` or `x` → `home/x`; absolute stays as is. `MkdirAll` when create_dirs is on; otherwise `Stat` and fail with "remote directory does not exist".
2. Open the source (staged file or original path; missing → permanent "source file not found").
3. Write to `dir/.<name>.<rand6>.part` through a counting reader that throttles progress events to 4 per second. Cancellation closes the remote file and removes the `.part`.
4. `Chmod(mode)` if set.
5. Commit by policy:
   - **rename:** try `Rename(part, name)`. SFTP v3 rename fails when the target exists (OpenSSH), so on failure with the target present try `name-1`, `name-2`, …, up to 999. That's atomic with no stat/rename race.
   - **overwrite:** `PosixRename(part, name)` (`posix-rename@openssh.com`). If the extension is missing: `Remove(name)` then `Rename`.
   - **skip:** `Stat(name)` before step 3. If it exists, mark skipped "already exists" without transferring.
6. Report the absolute final path.

**Retry classification:** `net.Error`, `io.EOF`, `io.ErrUnexpectedEOF`, `ssh: disconnect`, and `sftp` connection-lost errors are transient. Retry 3 times with 1/2/4 s backoff plus jitter, dropping the pool connection before each retry. `*sftp.StatusError` with `SSH_FX_PERMISSION_DENIED`/`NO_SUCH_FILE`/`FAILURE` on create, auth errors, `*HostKeyError`, and local file errors are permanent.

**Queues:** each profile has a buffered queue with 2 workers. FIFO dispatch gives capture order. A `context.CancelFunc` per job makes cancel O(1). On start, `store` marks `queued`/`uploading` rows as `failed: interrupted` (spec *Interrupted uploads after restart*).

### D9. Rules and naming are pure packages

- `rules.Compile([]store.Rule) (*Set, []error)` precompiles regexes (Go RE2) and globs (`path.Match` on lower-cased names). `Set.Evaluate(Entry) Decision{RuleName, Action, DirOverride, TemplateOverride}`. The same function powers "Test with current clipboard", which guarantees the dry run matches real behavior.
- `naming.Parse(tmpl) (*Template, error)` returns positioned errors. `Template.Render(Vars) string`, then transforms, then `Sanitize`. `RenderDir` sanitizes per segment and rejects `..`. Vars carry `Now`, `Name`, `Ext`, `Hash`, `Profile`, `Host`, and a `Counter func() int`. The counter is only incremented if the template uses `{counter}`, through `UPDATE counters ... RETURNING value` in the same transaction that inserts the history row. Randomness and clock are injectable for tests.
- The date `FORMAT` grammar is translated to Go's reference layout by a small longest-token-first scanner (`YYYY` before `YY`, `SSS` → `.000` without the dot).

### D10. Core controller and UI threading

`core.Controller` owns the store, the services, and a `State` snapshot (active profile, connection status, monitoring on/off, mode, queue counts, last error). Pipeline goroutines never touch view state. They emit typed events to a single `core` loop goroutine, which updates the store and then calls `notify(func())`. In production that's `win.Update`, which runs on the main thread and redraws. View actions (pause, switch profile, retry, save form) call Controller methods that do their I/O off the main thread and report back the same way. Views read only `State` and paged history slices cached in the view model; they never query SQLite during a build pass.

When the window is hidden it still exists (close-to-tray hides it), so `win.Update` stays valid. When close-to-tray is off, closing quits the app.

### D11. UI built from `mygo/ui` with an app theme

- Layout: `ui.Row` → `ui.Sidebar` (220 px; sections Activity / Servers / Rules / Naming / Settings, page id bound to `*string`) + `ui.Column` (header bar + page content, scrollable).
- Activity: `ui.Table` (virtualized) with a thumbnail, name, status badge, remote path, size, time, and a row context menu for actions. Above it, a summary strip, a search field, and segmented filters.
- Servers: a list and a `ui.Form` editor. Auth method is a `ui.Select`, the key path uses `mygo.Dialog.Open`, and secrets are password inputs that never prefill the stored value (they show "saved"). The host-key trust prompt is an `ui.AlertDialog`.
- Rules: a list with drag-to-reorder (`ui` list drag/drop) plus Move up/down buttons for keyboard users, and an edit dialog with condition sections.
- Naming: three template inputs, each with a live preview line, a token reference popover, and transform checkboxes. Validation errors show inline.
- Settings: grouped `ui.Form` fieldsets (General, Capture, Appearance, Notifications, Storage, About).
- Widget state that must survive rebuilds lives in view-model structs. Identity uses `c.Key` and `.Bind(&handle)`, following the mygo-maintenance rules.
- **Theme** (`view/theme.go`): copies of `ui.LightTheme()`/`ui.DarkTheme()` with a warm palette, chosen per frame from `c.Theme().Dark`. `mygo.Theme.SetSource` applies the saved Appearance before `App.Run` and on change.

  | Token | Light | Dark |
  |---|---|---|
  | Background | `#FAF9F5` | `#262624` |
  | Sidebar | `#F3F1EA` | `#1F1E1D` |
  | Surface / Hover / Pressed | `#F0EEE6` / `#E8E5DA` / `#DEDACD` | `#30302E` / `#3A3A37` / `#44443F` |
  | Border | `#E1DED3` | `#3E3E3A` |
  | Text / TextMuted | `#1F1E1D` / `#5F5E58` | `#F5F4EE` / `#A8A69C` |
  | Accent / Hover / Pressed | `#C96442` / `#B5583A` / `#A04D33` | `#D97757` / `#E08A6D` / `#C96442` |
  | AccentText | `#FFFFFF` | `#FFFFFF` |
  | Danger / Warning / Success | `#B83A2E` / `#B7791F` / `#3F7D4E` | `#E5675A` / `#D9A441` / `#6FB383` |

  Radius 8, Spacing 4, `InactiveAccent = Accent`, and `Inverse` set to keep toasts warm. Headings use a serif face (`Georgia`, which ships with Windows) to echo Claude's editorial look; body text uses the system UI font (Segoe UI). A unit test checks the WCAG contrast of Text and TextMuted against Background and Sidebar in both themes (spec *Warm-neutral visual style*). Measured for this palette: Text 15.8:1 light and 13.8:1 dark; TextMuted at least 5.4:1 on every surface. White on the accent is only 3.9:1 (light) and 3.1:1 (dark), so primary-button labels are semibold, which meets WCAG's 3:1 threshold for bold text. The app uses no Anthropic or Claude logos or names in its branding; only the palette and tone are borrowed.

### D12. Shell integration

- `main`: `RequestSingleInstanceLock()` → exit if false. `OnSecondInstance` → `win.Show(); win.Focus()`.
- Window: `StateKey: "main"`, min size 860×560, `Hidden: App.WasOpenedAtLogin()`. `OnClose` → hide when close-to-tray is on and we're not quitting (pattern from the MyGo tray docs), with a one-time "still running" notification. `OnBeforeQuit` asks for confirmation when uploads are in flight.
- Tray: `mygo.NewTray` with `ID: "main"`. Four state icons (monitoring, paused, uploading, error) are composed at startup from `resources/icon.png` with a colored corner badge (`image/draw`), so no extra artwork is needed for v1. The menu is rebuilt on state change: Pause/Resume, Upload clipboard now, Servers ▸ (radio items), Open SSHClipboard, Quit. `OnClick` shows the window.
- Notifications: `mygo.NewNotification` with the history id as the notification id. `App.OnNotificationClick` → show the window and navigate to Activity.
- Hotkey: `mygo.GlobalShortcut.Register(acc, core.UploadNow)`. To change it, register the new accelerator first, and only on success unregister the old one and persist (spec *Hotkey taken*).
- i18n: `mygo.json` gets `"i18n": {"dir": "locales", "source": "en"}`, and `go tool mygo generate` writes `locales/catalog_gen.go` (committed). `main` calls `mygo.App.SetLanguages("en")` so the OS language never changes the UI. Views call `locales.X(...)`; plural messages use ICU `{count, plural, ...}`. A test walks the views with `ui.NewTester` and asserts that `Texts()` contain no keys that are missing from the catalog.

### D13. Testing strategy

- `naming`, `rules`: table tests that cover every spec scenario.
- `store`: tests against a temp-dir DB covering migrations (including "newer version" and corrupt-file recovery). Skip with a clear message if `sqlite.Load()` can't get the native library (offline); CI pre-fetches it.
- `sshconn`, `uploader`: an **in-process SSH + SFTP server** (`x/crypto/ssh` server + `sftp.NewServer` on `127.0.0.1:0`, rooted in a temp dir) for auth methods, host-key unknown/changed, atomic rename, conflict policies, chmod, retry on injected disconnects, and cancel.
- `capture`: a fake clipboard source and fake watcher for debounce, classification priority, dedupe window, self-write and confidential filtering, and size cap.
- `view`: `ui.NewTester` flows (add server, edit rule, naming preview, theme switch, keyboard traversal).
- Gates: `CGO_ENABLED=0 go test ./...`, `GOOS=windows go build ./... && GOOS=windows go vet ./...`, `go tool mygo vet .`. Manual Windows smoke test from a checklist (screenshot, Explorer multi-copy, password manager copy, tray, hotkey, login start).

## Risks / Trade-offs

- [The 64 MiB per-representation cap in MyGo's Windows clipboard read rejects very large bitmaps (8K and multi-monitor screenshots)] → Specced as "content too large" and recorded in history. If users hit it, add a Win32 CF_DIB reader that streams into a PNG encoder (a later change).
- [`ReadImage` encodes PNG on the main thread, so a 4K screenshot may stall a frame for about 50–150 ms] → Acceptable for v1. Capture is debounced, and only one read happens per change. Measure it during smoke testing.
- [Polling every 150 ms adds constant (tiny) CPU wakeups] → One syscall per tick, no allocations. The `Watcher` interface allows moving to `AddClipboardFormatListener` without changing other packages.
- [Copy-back replaces a copied file list with text, which breaks a local paste of those files] → The setting is per profile, and the default is documented in the UI next to the toggle. Users can turn it off or use manual mode.
- [Clipboard content can be sensitive, and automatic upload leaks it to a server] → The confidential-format check is on by default, text is skipped by default, there's a pause switch in the header and tray, and history shows exactly what was sent.
- [SQLite plugin native library: `go test`/`go run` download it on first use; packaged builds bundle it] → CI caches the download. `store` isolates the driver (D5) as an escape hatch.
- [The Windows OpenSSH agent pipe isn't present when the service is disabled (the default on many machines)] → A clear error ("SSH agent is unavailable — start the 'OpenSSH Authentication Agent' service"), and the Servers form explains this next to the option.
- [Host-key prompts while the window is hidden could stall a queue indefinitely] → The queue is parked, not failed. A notification is shown, and the tray icon switches to the error badge until the user acts.
- [SFTP `Rename` semantics vary on non-OpenSSH servers, and some overwrite silently] → Before trying `Rename` under the rename policy, `Stat` the candidate name. Atomicity relies on OpenSSH behavior, which covers the stated target (Linux servers).
- [Template edits after captures are queued] → Names are rendered at enqueue time and stored on the history row, so later template edits don't change queued entries. Retry reuses the stored name, and the conflict policy still applies.

## Migration Plan

- No user data exists yet. This change replaces the template counter (`main.go`, `main_test.go`) and adds `i18n` to `mygo.json`.
- Release: `go tool mygo build` for `windows/amd64` and `windows/arm64`, then verify the bundle contains the SQLite native library and starts on a clean Windows 11 VM.
- Rollback: uninstall. The DB in the user data folder can be deleted safely. Later schema changes go through D5's migrations.

## Open Questions

- Final tray icon artwork. The composed badges are a placeholder that the specs don't depend on.
- Whether to also probe the Pageant named pipe in a later change. It doesn't affect v1 specs, because the auth method stays "agent" and only the dialer changes.
