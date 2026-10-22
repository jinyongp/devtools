// Package notices embeds third-party license notices in standalone distributions.
package notices

import _ "embed"

//go:embed NOTICE.txt
var Text string
