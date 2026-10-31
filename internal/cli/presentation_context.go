package cli

import "github.com/jinyongp/devtools/internal/cli/presentation"

func commandDisplayContext(command string, r Request) presentation.DisplayContext {
	c := presentation.DisplayContext{}
	switch command {
	case "variable set", "variable unset", "secret set", "secret unset", "secret generate":
		c.EnvironmentKnown, c.Environment = true, r.Options["env"]
		fallthrough
	case "env create", "env remove", "bundle set", "bundle rename", "bundle remove", "command add", "command update", "command remove", "command rename", "config tool add", "config tool update", "config tool remove", "config preset add", "config preset update", "config preset remove":
		if len(r.Args) > 0 {
			c.Target = r.Args[0]
		}
		if (command == "command rename" || command == "bundle rename") && len(r.Args) > 1 {
			c.Destination = r.Args[1]
		}
	}
	return c
}
