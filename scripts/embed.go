// Package scripts contains the shared release installer.
package scripts

import _ "embed"

//go:embed install.sh
var Installer string
