// Package store persists SSHClipboard's configuration and history in a
// single SQLite database (MyGo plugins/sqlite): embedded schema migrations,
// first-run defaults, corruption recovery, and typed repositories for
// settings, profiles, rules, naming templates, known hosts, counters, and
// history.
package store
