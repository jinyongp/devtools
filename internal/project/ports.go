package project

import "strings"

type Port struct {
	Port   *int  `toml:"port,omitempty"`
	Strict bool  `toml:"strict,omitempty"`
	Range  []int `toml:"range,omitempty"`
}

func ValidRange(r []int) bool { return len(r) == 2 && r[0] >= 1 && r[1] <= 65535 && r[0] <= r[1] }
func (p Port) Valid() bool {
	return (p.Port == nil || *p.Port >= 1 && *p.Port <= 65535) && (!p.Strict || p.Port != nil) && (p.Range == nil || ValidRange(p.Range))
}

type Binding struct {
	Port     string  `toml:"port,omitempty"`
	Profile  string  `toml:"profile,omitempty"`
	Instance string  `toml:"instance,omitempty"`
	Template *string `toml:"template,omitempty"`
}

func validBindings(c Command, ports map[string]Port) bool {
	seen := map[string]bool{}
	for _, name := range c.Serve {
		if _, ok := ports[name]; !ok || seen[name] {
			return false
		}
		seen[name] = true
	}
	for key, b := range c.Bind {
		if !requiredKeyPattern.MatchString(key) {
			return false
		}
		if b.Template != nil {
			if b.Port != "" || b.Profile != "" || b.Instance != "" || strings.ContainsRune(*b.Template, 0) {
				return false
			}
		} else if !ValidProfile(b.Port) || b.Profile != "" && !ValidProfile(b.Profile) || b.Instance != "" && !ValidProfile(b.Instance) && !strings.HasPrefix(b.Instance, "id:") {
			return false
		}
	}
	return true
}
