# Spec Delta

## Purpose

Provides the Windows desktop experience around clipboard uploading: a clean native window with light and dark themes, a tray presence that keeps the app out of the way, notifications, a global hotkey, and localizable text.

## ADDED Requirements

### Requirement: Main window layout
The main window SHALL have a sidebar with Activity, Servers, Rules, Naming, and Settings. A header SHALL show the active server selector, the connection status, a monitoring on/off switch, and an "Upload clipboard now" button. The window SHALL reopen at its last size and position.

#### Scenario: Navigate to Rules
- **WHEN** the user selects Rules in the sidebar
- **THEN** the content area shows the rule list, and the header stays visible

#### Scenario: Window position remembered
- **WHEN** the user moves and resizes the window and restarts the app
- **THEN** the window opens at the same size and position

### Requirement: Appearance setting
Settings SHALL offer the appearance options System (the default), Light, and Dark. A change SHALL apply immediately to every open window and persist across restarts. In System mode, the app SHALL follow the OS light/dark setting as it changes.

#### Scenario: Switch to dark
- **WHEN** the user selects Dark
- **THEN** the window redraws in the dark theme without a restart, and the app starts in dark after a restart

#### Scenario: Follow the OS
- **WHEN** appearance is System and the user switches Windows to dark mode
- **THEN** the app switches to the dark theme within one second

### Requirement: Warm-neutral visual style
Both themes SHALL use a warm-neutral palette with a terracotta accent, similar to Claude's interface: a warm off-white background in light mode, a warm charcoal background in dark mode, and the same terracotta for primary actions, selection, and focus. Body text SHALL have a contrast ratio of at least 4.5:1 against its background in both themes.

#### Scenario: Contrast check
- **WHEN** the theme's text and background colors are measured in light and in dark mode
- **THEN** each pair has a contrast ratio of at least 4.5:1, and so does muted text against the background

#### Scenario: Primary action color
- **WHEN** the Servers view shows its "Add server" primary button
- **THEN** the button is filled with the terracotta accent in both themes

### Requirement: Localizable text
All user-visible text SHALL come from a message catalog, with plural-aware messages for counts. English SHALL be the only shipped language and SHALL be shown whatever the OS language is.

#### Scenario: Non-English Windows
- **WHEN** the app runs on Windows with German as the display language
- **THEN** all window, tray, and notification text is in English

#### Scenario: Plurals
- **WHEN** the Activity summary shows 1 uploaded entry and then 2
- **THEN** it reads "1 file uploaded" and then "2 files uploaded"

### Requirement: Tray icon and menu
The system SHALL show a tray icon whose image reflects the state (monitoring, paused, uploading, error). Its tooltip SHALL name the app, the active profile, and the state. Its menu SHALL contain Pause or Resume monitoring, Upload clipboard now, a Servers submenu that marks the active profile, Open SSHClipboard, and Quit. Clicking the icon SHALL show and focus the main window.

#### Scenario: Pause from the tray
- **WHEN** the user selects Pause monitoring in the tray menu
- **THEN** monitoring stops, the icon changes to the paused image, the menu item now reads Resume monitoring, and the window header switch is off

#### Scenario: Error state
- **WHEN** the most recent upload failed
- **THEN** the tray icon shows the error image until the next successful upload or until the user opens the Activity view

### Requirement: Close to tray
When "Close to tray" is on (the default), closing the main window SHALL hide it while monitoring and uploads continue. The first time this happens, a notification SHALL say the app is still running in the tray. Quit from the tray menu or Alt+F4 with the setting off SHALL exit the app.

#### Scenario: Close keeps running
- **WHEN** "Close to tray" is on, the user closes the window, and then copies a screenshot
- **THEN** the window is hidden, and the screenshot is still captured and uploaded

#### Scenario: Quit with uploads in progress
- **WHEN** the user selects Quit while an upload is in progress
- **THEN** the app asks whether to quit anyway, and when the user confirms, it exits and records the unfinished entries as interrupted on the next start

### Requirement: Start at login
Settings SHALL offer "Start at login" (off by default). When the app starts because of login, it SHALL start hidden in the tray with monitoring in its last saved state.

#### Scenario: Login start
- **WHEN** "Start at login" is on and the user signs in to Windows
- **THEN** the app starts with only the tray icon visible, and monitoring is on if it was on before

### Requirement: Single instance
Only one instance SHALL run per Windows user. Launching the app again SHALL show and focus the existing main window instead of starting a second process.

#### Scenario: Launch twice
- **WHEN** the app is running in the tray and the user starts it again from the Start menu
- **THEN** the existing window is shown and focused, and no second tray icon appears

### Requirement: Notifications
Settings SHALL offer notifications: All, Errors only (the default), or Off. In All, a successful upload notifies with the remote path. In All and Errors only, a failure notifies with the error. Clicking a notification SHALL open the Activity view.

#### Scenario: Error notification
- **WHEN** notifications are Errors only and an upload fails with "connection refused"
- **THEN** a Windows notification shows the file name and "connection refused", and clicking it opens Activity

#### Scenario: Notifications off
- **WHEN** notifications are Off and an upload fails
- **THEN** no notification appears, and the failure still shows in Activity and the tray icon

### Requirement: Global hotkey
Settings SHALL let the user set or disable a system-wide hotkey for "Upload clipboard now" (default Ctrl+Alt+U). If the hotkey can't be registered because another app holds it, the system SHALL show an error and keep the previous working hotkey.

#### Scenario: Hotkey from another app
- **WHEN** the user presses Ctrl+Alt+U while another application has focus and the clipboard holds a screenshot
- **THEN** the screenshot is captured with a manual trigger and uploaded

#### Scenario: Hotkey taken
- **WHEN** the user sets a hotkey that another application has already registered
- **THEN** an error says the hotkey is unavailable, and the previous hotkey stays active

### Requirement: Keyboard operable
Every control in the main window SHALL be reachable and usable with the keyboard alone, with a visible focus indicator.

#### Scenario: Keyboard only
- **WHEN** the user moves through the Servers view with Tab and Shift+Tab and presses Enter or Space
- **THEN** every button, field, and switch receives focus in order, shows a focus ring, and can be activated
