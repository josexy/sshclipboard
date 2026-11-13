package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/plugins/sqlite"
)

// CaptureMode says when the clipboard is captured.
type CaptureMode string

const (
	CaptureAuto   CaptureMode = "auto"   // every clipboard change
	CaptureManual CaptureMode = "manual" // only "Upload clipboard now"
)

// Appearance is the theme source.
type Appearance string

const (
	AppearanceSystem Appearance = "system"
	AppearanceLight  Appearance = "light"
	AppearanceDark   Appearance = "dark"
)

// NotifyPolicy says which outcomes show a system notification.
type NotifyPolicy string

const (
	NotifyAll    NotifyPolicy = "all"
	NotifyErrors NotifyPolicy = "errors"
	NotifyOff    NotifyPolicy = "off"
)

// Settings are the app-wide settings. Each field is stored as a JSON value
// under its json tag in the settings table.
type Settings struct {
	Monitoring             bool         `json:"monitoring"`
	CaptureMode            CaptureMode  `json:"capture_mode"`
	ConfidentialProtection bool         `json:"confidential_protection"`
	DefaultAction          Action       `json:"default_action"`
	ActiveProfile          int64        `json:"active_profile"` // 0: none
	Appearance             Appearance   `json:"appearance"`
	Notifications          NotifyPolicy `json:"notifications"`
	RetentionCount         int          `json:"retention_count"`
	RetentionDays          int          `json:"retention_days"`
	StartAtLogin           bool         `json:"start_at_login"`
	StartHidden            bool         `json:"start_hidden"`
	CloseToTray            bool         `json:"close_to_tray"`
	CloseNoticeShown       bool         `json:"close_notice_shown"`
	Hotkey                 string       `json:"hotkey"`      // "" disables it
	StagingDir             string       `json:"staging_dir"` // "": <user data>/staging
}

// Setting keys, the json tags of Settings.
const (
	KeyMonitoring             = "monitoring"
	KeyCaptureMode            = "capture_mode"
	KeyConfidentialProtection = "confidential_protection"
	KeyDefaultAction          = "default_action"
	KeyActiveProfile          = "active_profile"
	KeyAppearance             = "appearance"
	KeyNotifications          = "notifications"
	KeyRetentionCount         = "retention_count"
	KeyRetentionDays          = "retention_days"
	KeyStartAtLogin           = "start_at_login"
	KeyStartHidden            = "start_hidden"
	KeyCloseToTray            = "close_to_tray"
	KeyCloseNoticeShown       = "close_notice_shown"
	KeyHotkey                 = "hotkey"
	KeyStagingDir             = "staging_dir"
)

// DefaultHotkey is the default "Upload clipboard now" accelerator.
const DefaultHotkey = "Ctrl+Alt+U"

// DefaultSettings returns the first-run settings.
func DefaultSettings() Settings {
	return Settings{
		Monitoring:             true,
		CaptureMode:            CaptureAuto,
		ConfidentialProtection: true,
		DefaultAction:          ActionUpload,
		Appearance:             AppearanceSystem,
		Notifications:          NotifyErrors,
		RetentionCount:         1000,
		RetentionDays:          30,
		CloseToTray:            true,
		Hotkey:                 DefaultHotkey,
	}
}

// settingsMap encodes s as the settings table's rows.
func settingsMap(s Settings) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	err = json.Unmarshal(b, &m)
	return m, err
}

// SettingsRepo reads and writes Settings.
type SettingsRepo struct{ s *Store }

// Load returns the stored settings, with defaults for missing keys.
func (r SettingsRepo) Load(ctx context.Context) (Settings, error) {
	rows, err := r.s.db.Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return Settings{}, err
	}
	m, err := settingsMap(DefaultSettings())
	if err != nil {
		return Settings{}, err
	}
	for _, row := range rows.Values {
		if _, known := m[str(row[0])]; known {
			m[str(row[0])] = json.RawMessage(str(row[1]))
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return Settings{}, err
	}
	out := DefaultSettings()
	if err := json.Unmarshal(b, &out); err != nil {
		return Settings{}, fmt.Errorf("settings: %w", err)
	}
	return out, nil
}

// Set stores one setting. key is one of the Key constants and value has
// its field's type.
func (r SettingsRepo) Set(ctx context.Context, key string, value any) error {
	m, err := settingsMap(DefaultSettings())
	if err != nil {
		return err
	}
	if _, known := m[key]; !known {
		return fmt.Errorf("settings: unknown key %q", key)
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	// Check that value decodes into the field, so a bad type fails here
	// rather than at the next Load.
	m[key] = b
	probe, _ := json.Marshal(m)
	if err := json.Unmarshal(probe, new(Settings)); err != nil {
		return fmt.Errorf("settings: %s: %w", key, err)
	}
	_, err = r.s.db.Execute(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, string(b))
	return err
}

// Save stores every setting in one transaction.
func (r SettingsRepo) Save(ctx context.Context, s Settings) error {
	stmts, err := settingsStatements(s)
	if err != nil {
		return err
	}
	_, err = r.s.db.Transaction(ctx, stmts)
	return err
}

func settingsStatements(s Settings) ([]sqlite.Statement, error) {
	m, err := settingsMap(s)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	stmts := make([]sqlite.Statement, 0, len(keys))
	for _, k := range keys {
		stmts = append(stmts, sqlite.Statement{
			SQL:  "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			Args: []any{k, string(m[k])},
		})
	}
	return stmts, nil
}
