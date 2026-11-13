# ssh-profiles Specification

## Purpose
TBD - created by archiving change add-ssh-proxy. Update Purpose after archive.

## Requirements

### Requirement: Proxy connection
A profile SHALL connect to its server either directly (the default) or through a SOCKS5 proxy given by host, port (default 1080), and an optional username. The proxy SHALL resolve the server's host name. Host keys SHALL be verified for the server's host and port, not the proxy's. The proxy password SHALL be stored like the profile's other secrets: only in the operating system's credential store, asked for when the store cannot provide it, and removed with the profile.

#### Scenario: Server reachable only through a proxy
- **WHEN** a profile has host "dev-box.internal", which only the proxy can resolve, and a SOCKS5 proxy at 127.0.0.1 port 1080, and the user selects Test
- **THEN** the app connects through the proxy, verifies the host key of "dev-box.internal", and reports success with the resolved remote directory

#### Scenario: Proxy refuses the sign-in
- **WHEN** the proxy rejects the profile's proxy username or password
- **THEN** the test reports a failure at the "connecting through the proxy" step that names the proxy, and uploads to that profile fail without retrying

#### Scenario: Proxy unreachable
- **WHEN** an upload starts while the proxy is not running
- **THEN** the upload is retried like other network failures, and if it still fails its error names the proxy

#### Scenario: Proxy password not stored
- **WHEN** a profile has a proxy username but the credential store has no proxy password for it
- **THEN** the app asks for the proxy password, uses it for this session only, and retries the uploads that failed for want of it

#### Scenario: Invalid proxy
- **WHEN** the user saves a profile with a SOCKS5 proxy whose host is empty or whose port is outside 1–65535
- **THEN** the profile is not saved and the invalid proxy field shows an error message

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
