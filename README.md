# SSHClipboard

SSHClipboard uploads what you copy on Windows to a Linux server over SSH, and
puts the remote path back on your clipboard so you can paste it straight into
your SSH session.

Take a screenshot with **Win+Shift+S**, and a moment later the clipboard holds
`/home/alice/clipboard/screenshot_20261112-153045.png`, ready to paste into a
terminal or hand to a command-line tool on the server that takes a file path.
Copied files work the same way. Text stays on your computer unless you ask for
it.

## What it does

- **Watches the clipboard** for screenshots, images, copied files, and text.
  It ignores its own writes and content that password managers mark as
  private.
- **Decides with rules** what to upload and where. By default, images and
  files upload, while text and files of 100 MB or more are skipped.
- **Names files from templates**, such as `screenshot_{datetime}.{ext}`, made
  safe for Linux.
- **Uploads over SFTP** in the background: it creates missing folders, writes
  each file under a temporary name and renames it once complete, retries
  dropped connections, and never overwrites a file unless you choose to.
- **Copies the remote path back** to the clipboard, ready to paste.
- **Keeps a history** you can search, with Retry, Upload anyway, and Copy
  remote path.

It runs in the tray. **Upload clipboard now** (the button, the tray menu, or
**Ctrl+Alt+U** from any app) uploads whatever is on the clipboard, even content
a rule would skip.

## Install

SSHClipboard runs on Windows 11, on x64 and Arm PCs.

1. Download the build for your PC, `windows-amd64` (most PCs) or
   `windows-arm64` (Arm PCs such as Snapdragon laptops).
2. Run the installer, or unpack the folder and start `SSHClipboard.exe`. Keep
   `Sqlitecore.dll` next to the executable: it is the app's database engine.

To build it yourself, with Go 1.27:

```sh
go tool mygo build -platform windows/amd64,windows/arm64
```

The bundles are written to `build/windows-amd64/` and `build/windows-arm64/`.

## First server setup

1. Open **Servers** and choose **Add server**.
2. Enter a **name** (such as `dev-box`), the **host**, and your **username**.
   The port defaults to 22 and the remote folder to `~/clipboard`.
3. Choose how to sign in:
   - **Password**: type it. It is kept in the Windows Credential Manager.
   - **Private key**: choose the key file, and type its passphrase if it has
     one. OpenSSH and PEM keys work.
   - **SSH agent**: uses the Windows **OpenSSH Authentication Agent** service,
     which must be running (Services → *OpenSSH Authentication Agent* →
     Startup type *Automatic*, then Start).
4. **Save**, then **Test connection**. The first connection shows the
   server's host key fingerprint: compare it with the server's
   (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`) and choose **Trust**.
   Hosts you already trust in `%USERPROFILE%\.ssh\known_hosts` are recognized
   without asking.

The first server you add becomes the active one. Switch servers from the window
header or the tray menu.

## Rules and file names

- [Capture rules](docs/rules.md): what is uploaded or skipped, conditions, the
  evaluation order, and folder and template overrides.
- [File naming](docs/naming.md): the template tokens, date formats, safe names,
  and dated folders.

## Defaults

| Setting | Default |
|---------|---------|
| Monitoring | On |
| Capture mode | Automatic |
| Hotkey for "Upload clipboard now" | Ctrl+Alt+U |
| Skip confidential content | On |
| Rules | "Skip text", then "Skip large files" (100 MB or more) |
| When no rule matches | Upload |
| Image file names | `screenshot_{datetime}.{ext}` |
| File names | `{name}.{ext}` |
| Text file names | `clip_{datetime}.{ext}` |
| Port | 22 |
| Remote folder | `~/clipboard` |
| Create missing folders | On |
| If the file exists | Rename |
| Copy remote path after upload | On |
| Clipboard text | `{path}` |
| Shell-quote paths | On |
| Notifications | Errors only |
| Appearance | System |
| Close to tray | On |
| Start at login | Off |
| History kept | 1000 entries, 30 days |

## Privacy

- **What you copy can leave your computer.** In automatic mode, every
  screenshot and copied file that the rules allow is uploaded to the active
  server. Text is skipped by default because it often holds secrets. Use the
  **Monitoring** switch or the tray's **Pause monitoring** when you work with
  sensitive material, or switch to **Manual** capture mode in Settings so that
  only the hotkey uploads.
- **Password-manager content is never captured.** Content marked private by
  password managers (KeePass, 1Password, Bitwarden) is ignored. You can turn
  this off in Settings, but there is little reason to.
- **Credentials stay in Windows.** Passwords and key passphrases are kept in
  the Windows Credential Manager, never in the app's database or logs. If the
  Credential Manager is unavailable, the app asks for them and keeps them only
  until it quits.
- **Host keys are verified.** Unknown or changed server keys need your
  approval, so uploads can't silently go to an impostor.
- **Local data.** The database (`sshclipboard.db`) and the staging folder for
  captured images and text live in `%APPDATA%\SSHClipboard`. History keeps at
  most 1000 entries for 30 days by default, and deleting an entry deletes its
  staged file. Files on servers are never deleted by the app.

## Troubleshooting

- **"The SSH agent is unavailable"**: start the *OpenSSH Authentication Agent*
  service, or use a key file.
- **"The host key of … changed"**: the server's key differs from the trusted
  one. If you reinstalled the server, choose **Replace**; otherwise, find out
  why before you do.
- **"This hotkey is unavailable"**: another app holds that key combination.
  Choose another in Settings; the previous one keeps working.
- **"content too large"**: the clipboard held an image or text over 64 MB, such
  as an 8K screenshot. Save it as a file and copy the file instead.
- **The database could not be read**: the app moves the damaged file aside as
  `sshclipboard.db.corrupt-<time>`, starts with defaults, and says where it
  kept the old file.

For testing a release, see the [manual test checklist](docs/manual-test.md).
