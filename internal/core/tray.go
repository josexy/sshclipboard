package core

// TrayItemKind is what a tray menu item does.
type TrayItemKind int

const (
	TrayItemPause     TrayItemKind = iota + 1 // Pause monitoring
	TrayItemResume                            // Resume monitoring
	TrayItemUploadNow                         // Upload clipboard now
	TrayItemServers                           // the Servers submenu
	TrayItemServer                            // a server in it, Checked when active
	TrayItemSeparator
	TrayItemOpen // Open SSHClipboard
	TrayItemQuit
)

// TrayItem is an item of the tray menu.
type TrayItem struct {
	Kind      TrayItemKind
	ProfileID int64  // TrayItemServer
	Label     string // TrayItemServer: the profile's name
	Checked   bool
	Submenu   []TrayItem
}

// TrayModel is what the tray shows for a state: the icon, the tooltip's
// parts, and the menu.
type TrayModel struct {
	Icon   TrayState
	Server string // the active profile's name, "" for none
	Items  []TrayItem
}

// Tray derives the tray from a state. The app rebuilds the native menu
// when the model changes.
func Tray(s *State) TrayModel {
	m := TrayModel{Icon: s.Tray}
	if s.Active != nil {
		m.Server = s.Active.Name
	}
	toggle := TrayItem{Kind: TrayItemPause}
	if !s.Settings.Monitoring {
		toggle.Kind = TrayItemResume
	}
	servers := TrayItem{Kind: TrayItemServers}
	for _, p := range s.Profiles {
		servers.Submenu = append(servers.Submenu, TrayItem{Kind: TrayItemServer, ProfileID: p.ID, Label: p.Name,
			Checked: s.Active != nil && s.Active.ID == p.ID})
	}
	m.Items = []TrayItem{toggle, {Kind: TrayItemUploadNow}, servers, {Kind: TrayItemSeparator},
		{Kind: TrayItemOpen}, {Kind: TrayItemQuit}}
	return m
}

// Equal tells whether two models show the same, so the app rebuilds the
// native tray only on change.
func (m TrayModel) Equal(o TrayModel) bool {
	if m.Icon != o.Icon || m.Server != o.Server || len(m.Items) != len(o.Items) {
		return false
	}
	for i := range m.Items {
		if !m.Items[i].equal(o.Items[i]) {
			return false
		}
	}
	return true
}

func (it TrayItem) equal(o TrayItem) bool {
	if it.Kind != o.Kind || it.ProfileID != o.ProfileID || it.Label != o.Label || it.Checked != o.Checked ||
		len(it.Submenu) != len(o.Submenu) {
		return false
	}
	for i := range it.Submenu {
		if !it.Submenu[i].equal(o.Submenu[i]) {
			return false
		}
	}
	return true
}
