package project

import (
	"strings"
	"time"
)

type ReadyProbe struct {
	Exec    []string `toml:"exec"`
	Timeout string   `toml:"timeout,omitempty"`
}

func (p ReadyProbe) Duration() time.Duration {
	if p.Timeout == "" {
		return 2 * time.Second
	}
	d, _ := time.ParseDuration(p.Timeout)
	return d
}

func (p ReadyProbe) Valid() bool {
	if len(p.Exec) == 0 || p.Exec[0] == "" {
		return false
	}
	for _, arg := range p.Exec {
		if strings.ContainsRune(arg, 0) {
			return false
		}
	}
	d := p.Duration()
	return d >= 10*time.Millisecond && d <= 30*time.Second
}
