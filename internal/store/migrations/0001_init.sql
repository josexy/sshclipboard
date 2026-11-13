-- Schema v1 (design D5). Times are Unix milliseconds. Booleans are 0/1.

-- Settings are JSON values under fixed keys, so a new toggle needs no
-- migration.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL CHECK (json_valid(value))
);

-- Profiles hold no secrets: passwords and passphrases live in the OS
-- credential store under profile/<id>/password|passphrase.
CREATE TABLE profiles (
  id              INTEGER PRIMARY KEY,
  name            TEXT    NOT NULL UNIQUE COLLATE NOCASE CHECK (length(trim(name)) > 0),
  host            TEXT    NOT NULL CHECK (length(trim(host)) > 0),
  port            INTEGER NOT NULL DEFAULT 22 CHECK (port BETWEEN 1 AND 65535),
  username        TEXT    NOT NULL CHECK (length(trim(username)) > 0),
  auth_method     TEXT    NOT NULL CHECK (auth_method IN ('password', 'key', 'agent')),
  key_path        TEXT    NOT NULL DEFAULT '',
  remote_dir      TEXT    NOT NULL DEFAULT '~/clipboard' CHECK (length(trim(remote_dir)) > 0),
  create_dirs     INTEGER NOT NULL DEFAULT 1 CHECK (create_dirs IN (0, 1)),
  conflict_policy TEXT    NOT NULL DEFAULT 'rename' CHECK (conflict_policy IN ('rename', 'overwrite', 'skip')),
  file_mode       INTEGER NULL CHECK (file_mode IS NULL OR file_mode BETWEEN 0 AND 4095),
  copy_back       INTEGER NOT NULL DEFAULT 1 CHECK (copy_back IN (0, 1)),
  copy_template   TEXT    NOT NULL DEFAULT '{path}',
  copy_separator  TEXT    NOT NULL DEFAULT ' ' CHECK (copy_separator IN (' ', char(10))),
  shell_quote     INTEGER NOT NULL DEFAULT 1 CHECK (shell_quote IN (0, 1)),
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);

-- Conditions are JSON so new condition types need no migration.
CREATE TABLE rules (
  id                INTEGER PRIMARY KEY,
  position          INTEGER NOT NULL,
  name              TEXT    NOT NULL,
  enabled           INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  action            TEXT    NOT NULL CHECK (action IN ('upload', 'skip')),
  conditions        TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(conditions)),
  dir_override      TEXT    NULL,
  template_override TEXT    NULL
);
CREATE INDEX rules_position ON rules (position);

CREATE TABLE naming (
  kind                 TEXT    PRIMARY KEY CHECK (kind IN ('image', 'file', 'text')),
  template             TEXT    NOT NULL CHECK (length(template) > 0),
  lowercase            INTEGER NOT NULL DEFAULT 0 CHECK (lowercase IN (0, 1)),
  spaces_to_underscore INTEGER NOT NULL DEFAULT 0 CHECK (spaces_to_underscore IN (0, 1))
);

CREATE TABLE known_hosts (
  host               TEXT    NOT NULL,
  port               INTEGER NOT NULL,
  key_type           TEXT    NOT NULL,
  key_b64            TEXT    NOT NULL,
  fingerprint_sha256 TEXT    NOT NULL,
  added_at           INTEGER NOT NULL,
  PRIMARY KEY (host, port)
);

-- {counter}: a per-profile, per-local-day sequence.
CREATE TABLE counters (
  profile_id INTEGER NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
  day        TEXT    NOT NULL,
  value      INTEGER NOT NULL CHECK (value >= 0),
  PRIMARY KEY (profile_id, day)
);

-- profile_name is a snapshot, so history stays readable after the profile
-- is deleted. remote_dir/remote_name are rendered at enqueue time.
CREATE TABLE history (
  id            INTEGER PRIMARY KEY,
  capture_id    TEXT    NOT NULL,
  captured_at   INTEGER NOT NULL,
  "trigger"     TEXT    NOT NULL CHECK ("trigger" IN ('auto', 'manual')),
  kind          TEXT    NOT NULL CHECK (kind IN ('image', 'file', 'text')),
  original_name TEXT    NOT NULL,
  local_path    TEXT    NOT NULL DEFAULT '',
  staged        INTEGER NOT NULL DEFAULT 0 CHECK (staged IN (0, 1)),
  thumb_path    TEXT    NOT NULL DEFAULT '',
  size          INTEGER NOT NULL DEFAULT 0,
  width         INTEGER NOT NULL DEFAULT 0,
  height        INTEGER NOT NULL DEFAULT 0,
  sha256        TEXT    NOT NULL DEFAULT '',
  profile_id    INTEGER NULL REFERENCES profiles (id) ON DELETE SET NULL,
  profile_name  TEXT    NOT NULL DEFAULT '',
  rule_name     TEXT    NOT NULL DEFAULT '',
  action        TEXT    NOT NULL CHECK (action IN ('upload', 'skip')),
  status        TEXT    NOT NULL CHECK (status IN ('queued', 'uploading', 'uploaded', 'skipped', 'failed', 'cancelled')),
  remote_dir    TEXT    NOT NULL DEFAULT '',
  remote_name   TEXT    NOT NULL DEFAULT '',
  remote_path   TEXT    NOT NULL DEFAULT '',
  attempts      INTEGER NOT NULL DEFAULT 0,
  error         TEXT    NOT NULL DEFAULT '',
  finished_at   INTEGER NULL
);
CREATE INDEX history_captured_at ON history (captured_at);
CREATE INDEX history_status ON history (status);
CREATE INDEX history_capture_id ON history (capture_id);
