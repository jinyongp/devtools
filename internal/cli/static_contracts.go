package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
)

const portPattern = `^([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])$`

func presentInput(name string) map[string]any { return map[string]any{"required": []string{name}} }

func staticContract(c *Command) {
	c.StaticRules = optionRules(c)
	for _, rule := range c.StaticRules {
		c.InputAllOf = append(c.InputAllOf, rule.schema())
	}
	for i := range c.Options {
		o := &c.Options[i]
		if c.Name == "doctor" && o.Name == "timeout" {
			o.Pattern = `^\+?(([0-9]+(\.[0-9]*)?|\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+$`
		}
		if c.Name == "proxy start" && o.Name == "port" || c.Name == "expose start" && o.Name == "serve-port" {
			o.Pattern = portPattern
		}
	}
	if c.Name == "proxy add" {
		c.Arguments[1].Pattern = portPattern
	}
	run := c.Run
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		for _, rule := range c.StaticRules {
			if e := rule.failure(r); e != nil {
				return nil, e
			}
		}
		return run(ctx, streams, r)
	}
}
