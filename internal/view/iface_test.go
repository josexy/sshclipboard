package view

import "github.com/josexy/sshclipboard/internal/core"

// The app's Controller is what the views command.
var _ Controller = (*core.Controller)(nil)
