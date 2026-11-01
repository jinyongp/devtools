package presentation

import (
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func outcomeHeading(message string) cliui.Block {
	word, rest, _ := strings.Cut(message, " ")
	role := cliui.Primary
	if word == "Preview:" {
		role = cliui.Info
	}
	for _, verb := range []string{"Created", "Updated", "Removed", "Applied", "Generated", "Initialized", "Completed", "Renamed", "Imported", "Exported"} {
		if word == verb {
			role = cliui.Success
			break
		}
	}
	if role == cliui.Primary {
		return cliui.Heading(cliui.Literal(message), 1)
	}
	if rest != "" {
		rest = " " + rest
	}
	return cliui.Heading(cliui.Text{Flow: cliui.Verbatim, Spans: []cliui.Span{{Text: word, Role: role}, {Text: rest}}}, 1)
}

func stateHeading(label, state string, role cliui.Role) cliui.Block {
	return cliui.Heading(cliui.Text{Flow: cliui.Verbatim, Spans: []cliui.Span{{Text: label + ": "}, {Text: state, Role: role}, {Text: "."}}}, 1)
}
