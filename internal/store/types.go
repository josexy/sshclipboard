package store

import "time"

// Kind is the kind of a captured entry.
type Kind string

const (
	KindImage Kind = "image"
	KindFile  Kind = "file"
	KindText  Kind = "text"
)

// Kinds lists every kind, in the order the UI shows them.
var Kinds = []Kind{KindImage, KindFile, KindText}

// Action is what a rule, or the default action, does with an entry.
type Action string

const (
	ActionUpload Action = "upload"
	ActionSkip   Action = "skip"
)

// Status is a history entry's place in its lifecycle.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusUploading Status = "uploading"
	StatusUploaded  Status = "uploaded"
	StatusSkipped   Status = "skipped"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Active reports whether the entry still waits for or runs a transfer.
// Active entries are never pruned or cleared.
func (s Status) Active() bool { return s == StatusQueued || s == StatusUploading }

// Trigger tells how a capture started.
type Trigger string

const (
	TriggerAuto   Trigger = "auto"
	TriggerManual Trigger = "manual"
)

// AuthMethod is how a profile authenticates.
type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
	AuthAgent    AuthMethod = "agent"
)

// ProxyType is how a profile reaches its server.
type ProxyType string

const (
	ProxyNone   ProxyType = "none" // connect directly
	ProxySOCKS5 ProxyType = "socks5"
)

// Proxy is the proxy a profile connects through. Its password lives in the
// OS credential store (package secret), like the profile's other secrets.
type Proxy struct {
	Type ProxyType
	Host string
	Port int
	// Username signs in to the proxy; "" when the proxy needs no sign-in.
	Username string
}

// Enabled tells whether connections go through the proxy.
func (p Proxy) Enabled() bool { return p.Type != "" && p.Type != ProxyNone }

// ConflictPolicy says what happens when the final remote name exists.
type ConflictPolicy string

const (
	ConflictRename    ConflictPolicy = "rename"
	ConflictOverwrite ConflictPolicy = "overwrite"
	ConflictSkip      ConflictPolicy = "skip"
)

// Rule names recorded in history for decisions no user rule made.
const (
	RuleDefault = "@default" // the default action decided
	RuleManual  = "@manual"  // "Upload clipboard now" bypassed the rules
)

// Reason codes recorded as a history entry's error. The view translates
// them; any other error text comes from the system or server and is shown
// as is.
const (
	ReasonNoMatchingRule   = "@no-matching-rule"
	ReasonFolder           = "@folder"
	ReasonTooLarge         = "@too-large"
	ReasonExists           = "@exists"
	ReasonInterrupted      = "@interrupted"
	ReasonSourceNotFound   = "@source-not-found"
	ReasonRemoteDirMissing = "@remote-dir-missing"
	ReasonHostKeyChanged   = "@host-key-changed"
	ReasonNoProfile        = "@no-profile"
	ReasonCancelled        = "@cancelled"
)

// Profile is an SSH server profile. It holds no secret material: passwords
// and passphrases, the proxy's included, live in the OS credential store
// (package secret).
type Profile struct {
	ID             int64
	Name           string
	Host           string
	Port           int
	Username       string
	AuthMethod     AuthMethod
	KeyPath        string
	Proxy          Proxy
	RemoteDir      string
	CreateDirs     bool
	ConflictPolicy ConflictPolicy
	// FileMode is the octal permission applied to uploaded files, or -1
	// to keep the server's default.
	FileMode      int
	CopyBack      bool
	CopyTemplate  string
	CopySeparator string
	ShellQuote    bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewProfile returns a profile with the defaults the Servers form starts
// from.
func NewProfile() Profile {
	return Profile{
		Port:           22,
		AuthMethod:     AuthPassword,
		Proxy:          Proxy{Type: ProxyNone, Port: 1080},
		RemoteDir:      "~/clipboard",
		CreateDirs:     true,
		ConflictPolicy: ConflictRename,
		FileMode:       -1,
		CopyBack:       true,
		CopyTemplate:   "{path}",
		CopySeparator:  " ",
		ShellQuote:     true,
	}
}

// Conditions are a rule's match conditions, combined with AND. A zero
// field matches everything. Pointers distinguish "unset" from zero.
type Conditions struct {
	Kinds      []Kind   `json:"kinds,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	Glob       string   `json:"glob,omitempty"`
	MinSize    *int64   `json:"min_size,omitempty"`
	MaxSize    *int64   `json:"max_size,omitempty"`
	MinWidth   *int     `json:"min_width,omitempty"`
	MaxWidth   *int     `json:"max_width,omitempty"`
	MinHeight  *int     `json:"min_height,omitempty"`
	MaxHeight  *int     `json:"max_height,omitempty"`
	TextRegex  string   `json:"text_regex,omitempty"`
}

// Rule is one entry of the ordered rule list.
type Rule struct {
	ID               int64
	Position         int
	Name             string
	Enabled          bool
	Action           Action
	Conditions       Conditions
	DirOverride      string
	TemplateOverride string
}

// Naming is the filename template of one kind and its transforms.
type Naming struct {
	Kind               Kind
	Template           string
	Lowercase          bool
	SpacesToUnderscore bool
}

// KnownHost is a host key the user trusted.
type KnownHost struct {
	Host        string
	Port        int
	KeyType     string
	KeyBase64   string
	Fingerprint string // SHA256:… as OpenSSH prints it
	AddedAt     time.Time
}

// HistoryEntry is one captured entry and its outcome.
type HistoryEntry struct {
	ID           int64
	CaptureID    string
	CapturedAt   time.Time
	Trigger      Trigger
	Kind         Kind
	OriginalName string
	LocalPath    string
	// Staged is true when LocalPath is a file the app wrote to the staging
	// folder (images and text), which the app deletes with the entry.
	// Files entries point at the user's original files, which it never
	// deletes.
	Staged      bool
	ThumbPath   string
	Size        int64
	Width       int
	Height      int
	SHA256      string
	ProfileID   int64 // 0 when no profile was active
	ProfileName string
	RuleName    string
	Action      Action
	Status      Status
	// RemoteDir and RemoteName are rendered when the entry is queued, so
	// later template edits don't change it and Retry reuses them.
	RemoteDir  string
	RemoteName string
	RemotePath string // absolute, once uploaded
	Attempts   int
	Error      string
	FinishedAt time.Time
}
