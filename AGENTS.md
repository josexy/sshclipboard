# AGENTS.md

SSHClipboard is a tray app that uploads clipboard screenshots and files to a
Linux server over SFTP and copies the remote path back. It targets Windows 11
(x64 and Arm); Linux and macOS builds are for development only. It is written
in Go 1.27 with [MyGo](https://github.com/egoist/mygo) v0.9.0 and its native
`ui` package (no web frontend), and stores data in SQLite through MyGo's
`plugins/sqlite`.

## Commands

```sh
CGO_ENABLED=0 go test ./...      # all tests
go tool mygo vet .               # go vet plus MyGo UI lifetime checks
gofmt -l .                       # must print nothing
go tool mygo generate            # after editing locales/en.json
go tool mygo i18n check .        # check the message catalog
go tool mygo build -platform windows/amd64,windows/arm64   # writes build/
```

Run the tests, vet, and gofmt before calling a change done. If the SQLite
native library can't be loaded (for example, when offline), store-backed tests
skip instead of failing. Run with `-v` if a run looks too quick.

## Layout

- `main.go`, `desktop.go`: the app's wiring, including the window, tray,
  notifications, hotkey, and clipboard source.
- `internal/core`: the `Controller`, which runs the capture → rules → naming
  → history → upload pipeline.
- `internal/capture` and `internal/clipwatch`: clipboard detection and
  capture.
- `internal/rules` and `internal/naming`: pure logic, with no UI and no
  clipboard.
- `internal/sshconn`, `internal/uploader`: SSH, SFTP, and upload queues.
  `sshconn/sshtest` is an in-process SSH/SFTP server and SOCKS5 proxy for
  tests.
- `internal/store`: SQLite storage. `store/storetest` opens throwaway
  databases.
- `internal/secret`: the OS credential store.
- `internal/view`: the main window's pages and theme.
- `locales`: the `en.json` catalog and the generated `catalog_gen.go`.

## Rules

- **Controller methods never block on I/O.** They post work to the
  Controller's single loop goroutine and report back through `done`
  callbacks on the UI thread (`Options.Notify`, which is `Window.Update` in
  the app). `core.State` is an immutable snapshot, so never modify it.
- **Views use the `view.Controller` interface.** A new command goes in that
  interface (`internal/view/app.go`), in `internal/core`, and in `fakeCtrl`
  (`internal/view/fake_test.go`).
- **Desktop calls stay in `main.go` and `desktop.go`.** Only these files touch
  `mygo.App`, windows, the tray, or dialogs. Other code reaches the desktop
  through `view.Platform` and `core.Hooks`.
- **Platform code uses `_windows.go` and `_other.go` files.** Windows
  behavior comes first.
- **UI code follows `.agents/skills/mygo-maintenance/SKILL.md`.** Never store
  a `ui.Element`, keep I/O off the UI thread, and check APIs against the
  pinned MyGo source.
- **Colors come only from `view.Palette`** in `internal/view/theme.go`.
  `TestPaletteContrast` checks their WCAG contrast.
- **Every user-visible string goes in `locales/en.json`.** After editing it,
  regenerate `catalog_gen.go` instead of editing that file by hand.
- **Schema changes go in a new migration.** Migration files are named
  `internal/store/migrations/NNNN_name.sql`, numbered without gaps, and must
  not contain `BEGIN` or `COMMIT`. Update `schema_test.go` with every schema
  change. New settings need no migration; add their defaults in
  `internal/store`.

## Invariants

- Passwords, passphrases, and proxy passwords live only in the OS credential
  store. They never appear in the database, logs, error messages, or
  prefilled form fields.
- An unknown or changed host key always waits for the user's approval.
- Remote writes go to a temporary file that is then renamed. Existing files
  are never overwritten unless the conflict policy allows it, and the app
  never deletes files on servers.
- Capture ignores the app's own clipboard writes and content that password
  managers mark as confidential.

## Docs checked by tests

- `readme_test.go` checks that the README's **Defaults** table matches the
  shipped defaults and that its links resolve. Change both together.
- The examples in `docs/rules.md` and `docs/naming.md` run as test cases.
- `docs/manual-test.md` lists the checks that need a real Windows desktop.
  Update it when the clipboard, tray, notifications, hotkey, or login behavior
  changes.

## Commits

Use an imperative subject line of about 72 characters or fewer. The body,
wrapped at 72, says what changed and why.
