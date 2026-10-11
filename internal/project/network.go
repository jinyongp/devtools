package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/dotenv"
	"github.com/jinyongp/devtools/internal/protocol"
)

var networkEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func reservedNetworkKey(k string) bool {
	return k == "profile" || k == "instance.alias" || k == "proxy"
}
func ExpandNetwork(input string, environment, reserved func(string) (string, bool)) (string, *protocol.Error) {
	return Expand(input, func(expression string) (string, bool) {
		if reservedNetworkKey(expression) {
			if reserved != nil {
				return reserved(expression)
			}
			return "", false
		}
		key, fallback, hasDefault := strings.Cut(expression, ":-")
		if !networkEnvKey.MatchString(key) {
			return "", false
		}
		value, ok := environment(key)
		if hasDefault && (!ok || value == "") {
			return fallback, true
		}
		return value, ok
	})
}
func ValidNetworkExpression(s string, host bool) bool {
	_, e := ExpandNetwork(s, func(string) (string, bool) { return "value", true }, func(k string) (string, bool) { return "value", host })
	return e == nil
}
func NetworkEnvironmentBacked(s string) bool {
	backed := false
	_, _ = ExpandNetwork(s, func(string) (string, bool) { backed = true; return "value", true }, func(string) (string, bool) { return "value", true })
	return backed
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// NetworkSources hashes tracked inputs without persisting their contents. A
// shell's environment belongs to an explicit activation, not daemon reloads.
func (p Context) NetworkSources() (string, *protocol.Error) {
	_, digest, e := p.networkEnvironment(nil, false)
	return digest, e
}
func (p Context) networkEnvironment(parent []string, load bool) (map[string]string, string, *protocol.Error) {
	env := map[string]string{}
	sources := []string{}
	if p.ConfigPath != "" {
		data, e := os.ReadFile(p.ConfigPath)
		if e != nil {
			return nil, "", ioError(p.ConfigPath)
		}
		sources = append(sources, Digest(data))
	}
	for _, file := range p.EnvFiles {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.Root, path)
		}
		data, e := os.ReadFile(path)
		if errors.Is(e, os.ErrNotExist) {
			sources = append(sources, "missing:"+file)
			continue
		}
		if e != nil {
			return nil, "", protocol.NewError("invalid_config", "Cannot read network env file.", 3, map[string]any{"file": file})
		}
		sources = append(sources, file+":"+Digest(data))
		if load {
			values, e := dotenv.ParseDotenv(string(data))
			if e != nil {
				e.Details["file"] = file
				return nil, "", e
			}
			for key, value := range values {
				if _, exists := env[key]; !exists {
					env[key] = value
				}
			}
		}
	}
	if load {
		for _, entry := range parent {
			key, value, ok := strings.Cut(entry, "=")
			if ok {
				env[key] = value
			}
		}
	}
	return env, Digest(sources), nil
}

type ResolvedNetwork struct {
	Context           Context
	SourceDigest      string
	EnvironmentBacked map[string]bool
}

// ResolvePorts evaluates only the served services. Unselected route variables
// do not become prerequisites for an ordinary command execution.
func (p Context) ResolvePorts(parent []string, services []string) (Context, *protocol.Error) {
	needed := false
	for _, name := range services {
		needed = needed || p.Ports[name].Expression != nil
	}
	if !needed {
		return p, nil
	}
	env, _, e := p.networkEnvironment(parent, true)
	if e != nil {
		return Context{}, e
	}
	out := p
	out.Ports = make(map[string]Port, len(p.Ports))
	for name, port := range p.Ports {
		out.Ports[name] = port
	}
	for _, name := range services {
		port, ok := p.Ports[name]
		if !ok {
			return Context{}, protocol.NewError("port_not_found", "The served port is not declared.", 3, nil)
		}
		port, e = resolveNetworkPort(name, port, func(key string) (string, bool) { value, ok := env[key]; return value, ok })
		if e != nil {
			return Context{}, e
		}
		out.Ports[name] = port
	}
	return out, nil
}

func resolveNetworkPort(name string, port Port, lookup func(string) (string, bool)) (Port, *protocol.Error) {
	if port.Expression == nil {
		return port, nil
	}
	value, e := ExpandNetwork(*port.Expression, lookup, nil)
	if e != nil {
		return Port{}, protocol.NewError("network_environment_required", "A referenced network environment variable is unavailable.", 3, map[string]any{"field": "ports." + name + ".port"})
	}
	port.Expression = nil
	if value == "" || value == "0" {
		port.Port = nil
		if port.Strict {
			return Port{}, protocol.NewError("invalid_config", "Strict ports require a nonzero number.", 3, nil)
		}
	} else {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return Port{}, protocol.NewError("invalid_config", "Network port must resolve to a number in 1..65535.", 3, map[string]any{"field": "ports." + name + ".port"})
		}
		port.Port = &n
	}
	return port, nil
}

func (p Context) ResolveNetwork(parent []string, alias string, selected ...string) (ResolvedNetwork, *protocol.Error) {
	env, source, e := p.networkEnvironment(parent, true)
	if e != nil {
		return ResolvedNetwork{}, e
	}
	out := ResolvedNetwork{Context: p, SourceDigest: source, EnvironmentBacked: map[string]bool{}}
	out.Context.Ports = map[string]Port{}
	out.Context.Proxies = map[string]Proxy{}
	lookup := func(key string) (string, bool) { v, ok := env[key]; return v, ok }
	names := map[string]bool{}
	portNames := map[string]bool{}
	for _, name := range selected {
		proxy, ok := p.Proxies[name]
		if !ok {
			return ResolvedNetwork{}, protocol.NewError("route_not_found", "The selected proxy is not declared.", 3, nil)
		}
		names[name] = true
		portNames[proxy.Port] = true
	}
	for name, port := range p.Ports {
		if len(selected) > 0 && !portNames[name] {
			out.Context.Ports[name] = port
			continue
		}
		port, e = resolveNetworkPort(name, port, lookup)
		if e != nil {
			return ResolvedNetwork{}, e
		}
		out.Context.Ports[name] = port
	}
	for name, proxy := range p.Proxies {
		if len(selected) > 0 && !names[name] {
			out.Context.Proxies[name] = proxy
			continue
		}
		expression := p.Ports[proxy.Port].Expression
		out.EnvironmentBacked[name] = NetworkEnvironmentBacked(proxy.Host) || expression != nil && NetworkEnvironmentBacked(*expression)
		proxy.Host, e = ExpandNetwork(proxy.Host, lookup, func(key string) (string, bool) {
			switch key {
			case "profile":
				return p.Profile, true
			case "proxy":
				return name, true
			case "instance.alias":
				return alias, alias != ""
			}
			return "", false
		})
		if e != nil {
			return ResolvedNetwork{}, protocol.NewError("network_environment_required", "A referenced network environment variable or instance alias is unavailable.", 3, map[string]any{"field": "proxies." + name + ".host"})
		}
		out.Context.Proxies[name] = proxy
	}
	return out, nil
}
