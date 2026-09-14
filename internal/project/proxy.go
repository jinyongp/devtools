package project

import "strings"

type Proxy struct {
	Host string `toml:"host"`
	Port string `toml:"port"`
}

func (p Proxy) Valid(ports map[string]Port) bool {
	if !ValidProfile(p.Port) {
		return false
	}
	if _, ok := ports[p.Port]; !ok {
		return false
	}
	expanded, err := Expand(p.Host, func(key string) (string, bool) {
		switch key {
		case "profile", "instance.alias", "proxy":
			return "value", true
		default:
			return "", false
		}
	})
	return err == nil && ValidProxyHost(expanded)
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
