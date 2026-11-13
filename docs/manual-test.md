# Manual test checklist

Run this on a **Windows 11** machine against a **Linux** server reachable over
SSH, with a build from `go tool mygo build -platform windows/amd64` (or
`windows/arm64` on an Arm PC). Start each run from a fresh database: quit the
app and delete `%APPDATA%\SSHClipboard\sshclipboard.db`.

The automated tests cover the logic behind every item (`go test ./...`); these
steps check what only a real desktop can: the Windows clipboard, the tray,
notifications, the global hotkey, login items, and a real OpenSSH server.

Record each result as **pass**, **fail** (with what happened), or **n/a**, with
the date and the build's version.

| Build | Windows version | Server | Tester | Date |
|-------|-----------------|--------|--------|------|
| | | | | |

## Setup

1. Start the app. The window shows **Activity**, and **Servers** says to add a
   server.
2. **Servers → Add server**: name `dev-box`, the server's host and user, a
   password. Leave the port at 22 and the folder at `~/clipboard`. **Save**.
   `dev-box` becomes the active server in the header.
3. **Test connection**: the first connection asks to verify the host key (see
   item 8), then reports `Connected. Uploads go to /home/<user>/clipboard`.

## Checklist

| # | Steps | Expected | Result |
|---|-------|----------|--------|
| 1 | Take a region screenshot with **Win+Shift+S**. | Within a second, Activity shows an image entry `screenshot_YYYYMMDD-HHmmss.png`, uploaded, with a thumbnail; the file is in `~/clipboard` on the server; the clipboard holds its absolute path. | |
| 2 | In File Explorer, select two files and a folder, and press **Ctrl+C**. | Both files upload; the folder is recorded as skipped, "folders are not supported". The clipboard holds both paths separated by a space. | |
| 3 | In a password manager (KeePass, 1Password, or Bitwarden), copy a password. | Nothing is captured or recorded. | |
| 4 | Copy text from an editor. | An entry is recorded as skipped by "Skip text"; nothing is uploaded. **Upload anyway** uploads it as `clip_….txt`. | |
| 5 | Copy a screenshot, then paste (**Ctrl+V**) into an SSH session to the server, e.g. after `ls -l `. | The pasted remote path names the uploaded file, and `ls` lists it. With a file name containing a space, the path is single-quoted. | |
| 6 | Right-click the tray icon → **Pause monitoring**. Copy a file. Then **Resume monitoring**. | The icon shows the paused badge (two bars), the header switch is off, the menu reads **Resume monitoring**, and nothing is captured, also after resuming. | |
| 7 | With another application focused and a screenshot on the clipboard, press **Ctrl+Alt+U**. Then set the hotkey to one another app holds (e.g. **Ctrl+C**) in Settings. | The screenshot uploads with trigger "Manual". Setting a taken hotkey shows "This hotkey is unavailable…" and Ctrl+Alt+U keeps working. | |
| 8 | Host key, first use: before trusting `dev-box`, close the window to the tray and copy a screenshot. Then open the app. Change the server's host key (e.g. regenerate it) and copy again. | First use: a notification asks to verify the server, the upload waits, nothing is sent; opening the app shows the key type and SHA-256 fingerprint; **Trust** uploads it. Changed key: the upload fails with "host key changed", a warning shows the trusted and new fingerprints, and **Replace** then **Retry** uploads it. | |
| 9 | Reboot the server (or restart `sshd` and kill the session). After it is back, copy a screenshot. | The status shows the connection lost, then reconnects by itself and uploads; the status returns to Connected. | |
| 10 | Close the window (title bar ×). Copy a screenshot. | The window hides, the first time a notification says the app is still running, and the screenshot still uploads. Clicking the tray icon shows the window. | |
| 11 | With the app in the tray, start it again from the Start menu. | The existing window shows and takes the focus; no second tray icon appears. | |
| 12 | Settings → **Start at login** on. Sign out and in. | The app starts with only the tray icon; monitoring is in its last state. | |
| 13 | Settings → Appearance **Dark**, **Light**, then **System**, and switch Windows between light and dark mode. | The window redraws at once in each; in System it follows Windows within a second; after a restart it keeps the choice. | |
| 14 | Start a large upload (a file of a few hundred MB) and choose **Quit** in the tray menu. | The app asks to quit while uploading; after **Quit** and a restart, the entry shows failed "interrupted" with **Retry**. | |
| 15 | Cancel a large upload from Activity while it is about 30% done. | It stops within a second, shows Cancelled, and no file (not even a `.part`) is left on the server. | |
| 16 | Move and resize the window, quit from the tray, and start again. | The window opens at the same size and position. | |
| 17 | Copy a very large bitmap (a full 8K or multi-monitor screenshot). | It is recorded as skipped, "content too large", with no staged file. | |
| 18 | Set notifications to **Errors only**, stop the server's SSH service, and copy a screenshot. Click the notification. | After the retries a notification names the file and "connection refused"; clicking it opens Activity; the tray shows the error badge until the next success or until Activity is opened. | |

## Notes

Record failures, timings (for item 1, the delay before the entry appears), and
anything unexpected here.
