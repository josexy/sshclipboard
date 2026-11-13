# config-storage Specification

## Purpose
Persists the app's configuration and history in a local SQLite database, so settings survive restarts and upgrades, and recovers safely when the database is missing, outdated, or damaged.

## Requirements

### Requirement: Single local database
The system SHALL store all configuration and history in one SQLite database file, `sshclipboard.db`, in the app's per-user data folder. This covers settings, profiles without secrets, rules, naming templates, trusted host keys, daily counters, and history.

#### Scenario: Database location
- **WHEN** the app runs for Windows user `alice`
- **THEN** its configuration is in `sshclipboard.db` under alice's app data folder for SSHClipboard, and other Windows users don't see it

### Requirement: Durable saves
The system SHALL persist each committed configuration change (a saved form or a toggled setting) before confirming it in the UI. A change that cannot be persisted SHALL show an error and leave the previous value in effect.

#### Scenario: Survives a crash
- **WHEN** the user turns on "Start at login" and the process is killed a moment later
- **THEN** "Start at login" is still on after the next start

#### Scenario: Write fails
- **WHEN** saving a rule fails because the disk is full
- **THEN** the UI shows the error, and the rule list is unchanged

### Requirement: First-run defaults
When no database exists, the system SHALL create one and fill it with defaults: monitoring on, automatic capture mode, theme follows the system, default naming templates, the default rules and default action, notifications for errors only, retention of 1000 entries and 30 days, and no profiles.

#### Scenario: Fresh start
- **WHEN** the app starts for the first time
- **THEN** the database is created, the Servers view asks the user to add a server, and the Rules view lists "Skip text" and "Skip large files"

### Requirement: Versioned schema migrations
The database SHALL record its schema version. On startup, the system SHALL apply each pending migration in order, each one atomically. If a migration fails, the database SHALL stay at its previous version, and the app SHALL show the error instead of running with a partial schema.

#### Scenario: Upgrade from an older version
- **WHEN** an app with schema version 3 opens a database at version 1
- **THEN** migrations 2 and 3 are applied in order, and the database records version 3

#### Scenario: Database from a newer app
- **WHEN** the app with schema version 3 opens a database at version 5
- **THEN** the app does not modify the database, and it shows that the data was created by a newer version

### Requirement: Recovery from an unreadable database
If the database file cannot be opened or fails its integrity check, the system SHALL rename it to `sshclipboard.db.corrupt-<timestamp>`, create a new database with defaults, and tell the user where the old file was kept.

#### Scenario: Corrupt file
- **WHEN** `sshclipboard.db` contains random bytes at startup
- **THEN** it is renamed with a `.corrupt-` suffix, the app starts with defaults, and a message shows the renamed file's path

### Requirement: Secrets excluded
The database SHALL NOT contain passwords, passphrases, or private key material. Profiles store only the path of a private key file and a reference to their secret in the OS credential store.

#### Scenario: Inspect the database
- **WHEN** the database file is opened with any SQLite tool after profiles with passwords and key passphrases were saved
- **THEN** no column contains a password, a passphrase, or private key bytes

### Requirement: Staging folder location
The system SHALL keep staged image and text files in a staging folder, by default a `staging` folder inside the app's per-user data folder. The user can choose another folder in Settings. Existing staged files stay where they are, and new captures use the new folder.

#### Scenario: Change staging folder
- **WHEN** the user sets the staging folder to `D:\clip-staging`
- **THEN** the next image is staged in `D:\clip-staging`, and earlier history entries still open their files from the old location
