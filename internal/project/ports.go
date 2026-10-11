package project

import (
	"bytes"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"strings"
)

type Port struct {
	Port       *int    `toml:"port,omitempty"`
	Strict     bool    `toml:"strict,omitempty"`
	Range      []int   `toml:"range,omitempty"`
	Expression *string `toml:"-"`
}

func (p *Port) UnmarshalTOML(data []byte) error {
	var raw struct {
		Port   any   `toml:"port"`
		Strict bool  `toml:"strict"`
		Range  []int `toml:"range"`
	}
	if e := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw); e != nil {
		return e
	}
	p.Strict, p.Range = raw.Strict, raw.Range
	switch v := raw.Port.(type) {
	case nil:
	case int64:
		n := int(v)
		p.Port = &n
	case string:
		p.Expression = &v
	default:
		return fmt.Errorf("invalid port declaration")
	}
	return nil
}

func ValidRange(r []int) bool { return len(r) == 2 && r[0] >= 1 && r[1] <= 65535 && r[0] <= r[1] }
func (p Port) Valid() bool {
	return (p.Port == nil || *p.Port >= 1 && *p.Port <= 65535) && (!p.Strict || p.Port != nil || p.Expression != nil) && (p.Range == nil || ValidRange(p.Range)) && (p.Expression == nil || ValidNetworkExpression(*p.Expression, false))
}

type Binding struct {
	Proxy    string  `toml:"proxy,omitempty"`
	Format   string  `toml:"format,omitempty"`
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
			if b.Port != "" || b.Proxy != "" || b.Format != "" || b.Profile != "" || b.Instance != "" || strings.ContainsRune(*b.Template, 0) {
				return false
			}
		} else if (b.Proxy != "" && (!ValidProfile(b.Proxy) || b.Port != "" || b.Format != "")) || (b.Proxy == "" && (!ValidProfile(b.Port) || b.Format != "" && b.Format != "number" && b.Format != "url")) || b.Profile != "" && !ValidProfile(b.Profile) || b.Instance != "" && !ValidProfile(b.Instance) && !strings.HasPrefix(b.Instance, "id:") {
			return false
		}
	}
	return true
}
