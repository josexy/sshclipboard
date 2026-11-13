# Spec Delta

## Purpose

Lets the user define the Linux servers that clipboard content is uploaded to, authenticate to them safely, and choose which server is the active upload target at any moment.

## ADDED Requirements

### Requirement: Manage server profiles
The system SHALL let the user create, edit, duplicate, and delete server profiles. Each profile has a display name (unique, required), a host (required), a port (1–65535, default 22), a username (required), an authentication method, and a default remote directory (required, default `~/clipboard`).

#### Scenario: Create a profile with defaults
- **WHEN** the user creates a profile with name "dev-box", host "10.0.0.5", and username "alice", and leaves the other fields unchanged
- **THEN** the profile is saved with port 22 and remote directory `~/clipboard`, and it appears in the server list

#### Scenario: Reject invalid profile
- **WHEN** the user saves a profile with an empty host, a port of 70000, or a name that another profile already uses
- **THEN** the profile is not saved and the invalid field shows an error message

#### Scenario: Delete the active profile
- **WHEN** the user deletes the profile that is currently active and confirms
- **THEN** the profile, its stored secrets, and its trusted host key are removed, no profile is active, and automatic uploads stop until another profile is activated

### Requirement: Authentication methods
A profile SHALL authenticate with exactly one method: password, private key file (with optional passphrase), or the Windows OpenSSH authentication agent. Keyboard-interactive prompts SHALL be answered with the stored password when the method is password.

#### Scenario: Private key with passphrase
- **WHEN** a profile uses private key `C:\Users\alice\.ssh\id_ed25519`, the key is encrypted, and the stored passphrase is correct
- **THEN** the connection authenticates with that key

#### Scenario: Agent not running
- **WHEN** a profile uses the agent method and the Windows OpenSSH agent service is not running
- **THEN** the connection fails with an error saying the SSH agent is unavailable

#### Scenario: Unreadable key file
- **WHEN** a profile's private key file does not exist or cannot be parsed
- **THEN** the connection fails with an error that names the key file and the reason

### Requirement: Secret storage
The system SHALL store passwords and key passphrases only in the operating system's credential store, keyed by profile, and never in the configuration database, logs, or history. If the credential store is unavailable, the system SHALL ask for the secret when connecting and keep it in memory for the session only.

#### Scenario: Secret saved outside the database
- **WHEN** the user saves a profile with a password
- **THEN** the password is in the OS credential store, and no table in the configuration database contains it

#### Scenario: Credential store unavailable
- **WHEN** a profile needs a password and the credential store returns an error
- **THEN** the app asks for the password when it connects, uses it for this session only, and shows that it will not be remembered

### Requirement: Host key verification
The system SHALL verify the server host key on every connection. On first connection to a host and port, it SHALL show the key type and SHA-256 fingerprint and connect only after the user accepts. A key that differs from the trusted one SHALL block the connection until the user explicitly replaces the trusted key.

#### Scenario: First connection
- **WHEN** the app connects to a host it has no trusted key for
- **THEN** it shows the key type and SHA-256 fingerprint, and it saves the key and connects only if the user selects Trust

#### Scenario: Changed host key
- **WHEN** the server presents a key that differs from the trusted key for that host and port
- **THEN** the connection is refused, any uploads for it fail with a "host key changed" error, and a warning shows the old and new fingerprints along with a Replace action

#### Scenario: Unattended first connection
- **WHEN** an automatic upload needs a host that has no trusted key and the main window is hidden
- **THEN** the upload waits, a notification asks the user to open the app to verify the server, and nothing is sent until the key is trusted

### Requirement: Test connection
The system SHALL let the user test a profile. The test connects, authenticates, checks that the default remote directory exists or can be created, and checks that it is writable. It reports success or the first failing step.

#### Scenario: Successful test
- **WHEN** the user selects Test on a profile with valid settings
- **THEN** the app reports success with the resolved absolute remote directory, for example `/home/alice/clipboard`

#### Scenario: Directory not writable
- **WHEN** the test authenticates but cannot create or write to the remote directory
- **THEN** the app reports a failure at the "remote directory" step with the server's error message

### Requirement: Active profile selection
At most one profile SHALL be active at a time. The user SHALL be able to switch the active profile from the main window and from the tray menu, and the choice SHALL persist across restarts. Items captured after a switch go to the newly active profile.

#### Scenario: Switch from the tray
- **WHEN** the user selects "prod-box" under the tray menu's server list while "dev-box" is active
- **THEN** "prod-box" becomes active, the window header and tray tooltip show "prod-box", and the next captured item uploads to "prod-box"

#### Scenario: Restart keeps the active profile
- **WHEN** the app restarts after "prod-box" was the active profile
- **THEN** "prod-box" is active again without user action

#### Scenario: In-flight uploads during a switch
- **WHEN** the user switches the active profile while an upload to the previous profile is in progress
- **THEN** that upload finishes to the previous profile, and only new items use the new profile

### Requirement: Connection status
The system SHALL show the active profile's connection state (disconnected, connecting, connected, error) in the main window and the tray. It SHALL keep an idle connection alive and reconnect automatically when the next upload needs it.

#### Scenario: Server reboot
- **WHEN** the connection drops because the server rebooted, and a new item is captured after the server is back
- **THEN** the app reconnects without user action and uploads the item, and the status returns to connected
