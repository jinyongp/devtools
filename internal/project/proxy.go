package project

import (
	"github.com/jinyongp/devtools/internal/network"
	"strings"
)

type Proxy struct {
	Host       string             `toml:"host"`
	Port       string             `toml:"port"`
	Listener   string             `toml:"listener,omitempty"`
	DNS        string             `toml:"dns,omitempty"`
	HostPolicy network.HostPolicy `toml:"host_policy,omitempty" json:",omitempty"`
}

func (p Proxy) Valid(ports map[string]Port) bool {
	if !p.HostPolicy.Valid() {
		return false
	}
	if p.Listener != "" && !ValidProfile(p.Listener) || p.DNS != "" && p.DNS != "auto" && p.DNS != "hosts" && p.DNS != "external" {
		return false
	}
	if !ValidProfile(p.Port) {
		return false
	}
	if _, ok := ports[p.Port]; !ok {
		return false
	}
	if !ValidNetworkExpression(p.Host, true) {
		return false
	}
	expanded, err := ExpandNetwork(p.Host, func(string) (string, bool) { return "value", true }, func(string) (string, bool) { return "value", true })
	return err == nil && network.ValidHost(expanded)
}

func ValidProxyHost(host string) bool {
	if len(host) > 253 || !strings.HasSuffix(host, ".localhost") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if char < 'a' || char > 'z' {
				if char < '0' || char > '9' {
					if char != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}
