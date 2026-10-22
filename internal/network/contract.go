// Package network defines identities and endpoint calculations shared by the
// registry, proxy, execution bindings, and exposure control plane.
package network

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const DefaultListener = "default"

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RouteRef is the stable owner of activation, hosts references, and exposure pins.
// A global name is a lookup key; it is deliberately absent from its identity.
type RouteRef struct {
	Scope      string `json:"scope"`
	Profile    string `json:"profile,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	Proxy      string `json:"proxy,omitempty"`
	RouteID    string `json:"route_id,omitempty"`
}

func (r RouteRef) Valid() bool {
	switch r.Scope {
	case "project":
		return namePattern.MatchString(r.Profile) && len(r.InstanceID) == 32 && isHex(r.InstanceID) && namePattern.MatchString(r.Proxy) && r.RouteID == ""
	case "global":
		return uuidPattern.MatchString(r.RouteID) && r.Profile == "" && r.InstanceID == "" && r.Proxy == ""
	}
	return false
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func (r RouteRef) Key() string {
	if r.Scope == "global" {
		return "global/" + r.RouteID
	}
	return "project/" + r.Profile + "/" + r.InstanceID + "/" + r.Proxy
}

type Listener struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	HTTPReservation  string `json:"http_reservation,omitempty"`
	HTTPSReservation string `json:"https_reservation,omitempty"`
	HTTPHost         string `json:"http_host,omitempty"`
	HTTPSHost        string `json:"https_host,omitempty"`
	Generation       string `json:"generation"`
	LegacyLoopback   bool   `json:"legacy_loopback,omitempty"`
}

func ListenerName(name string) string {
	if name == "" {
		return DefaultListener
	}
	return name
}

func (l Listener) Valid() bool {
	return uuidPattern.MatchString(l.ID) && uuidPattern.MatchString(l.Generation) && namePattern.MatchString(l.Name) &&
		(l.HTTPReservation != "" || l.HTTPSReservation != "") &&
		(!l.LegacyLoopback || l.Name == DefaultListener && l.HTTPHost == "127.0.0.1") &&
		validBind(l.HTTPHost, l.HTTPReservation) && validBind(l.HTTPSHost, l.HTTPSReservation)
}

func validBind(host, reservation string) bool {
	if reservation == "" {
		return host == ""
	}
	_, err := netip.ParseAddr(host)
	return err == nil && namePattern.MatchString(reservation)
}

// ParseAddress requires an IP literal; configuring a socket never triggers DNS.
func ParseAddress(address string) (string, int, error) {
	a, err := netip.ParseAddrPort(address)
	if err != nil || a.Port() == 0 {
		return "", 0, fmt.Errorf("expected IP:port with port 1..65535")
	}
	return a.Addr().Unmap().String(), int(a.Port()), nil
}

func ValidHost(host string) bool {
	if host == "" || len(host) > 253 || host != strings.ToLower(host) {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func RouteURL(host string, httpPort, httpsPort int) (string, error) {
	if !ValidHost(host) {
		return "", fmt.Errorf("invalid route hostname")
	}
	scheme, port := "http", httpPort
	if httpsPort != 0 {
		scheme, port = "https", httpsPort
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("listener port is unavailable")
	}
	authority := host
	if !(scheme == "http" && port == 80 || scheme == "https" && port == 443) {
		authority = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return (&url.URL{Scheme: scheme, Host: authority}).String(), nil
}

func BackendURL(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

type Activation struct {
	Route             RouteRef `json:"route"`
	Mode              string   `json:"mode"`
	Host              string   `json:"host,omitempty"`
	Listener          string   `json:"listener,omitempty"`
	DNS               string   `json:"dns,omitempty"`
	ConfigDigest      string   `json:"config_digest,omitempty"`
	SourceDigest      string   `json:"source_digest,omitempty"`
	NetworkDigest     string   `json:"network_digest,omitempty"`
	EnvironmentBacked bool     `json:"environment_backed,omitempty"`
}

func (a Activation) Valid() bool {
	if !a.Route.Valid() {
		return false
	}
	if a.Mode == "disabled" {
		return a.Host == "" && a.ConfigDigest == "" && a.SourceDigest == "" && a.NetworkDigest == "" && a.Listener == "" && a.DNS == "" && !a.EnvironmentBacked
	}
	return a.Mode == "active" && ValidHost(a.Host) && namePattern.MatchString(a.Listener) && (a.DNS == "auto" || a.DNS == "hosts" || a.DNS == "external") && len(a.ConfigDigest) == 64 && len(a.SourceDigest) == 64 && len(a.NetworkDigest) == 64 && isHex(a.ConfigDigest+a.SourceDigest+a.NetworkDigest)
}

type GlobalRoute struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Host        string     `json:"host"`
	Reservation string     `json:"reservation"`
	Listener    string     `json:"listener"`
	DNS         string     `json:"dns"`
	HostPolicy  HostPolicy `json:"host_policy,omitempty"`
}

func (r GlobalRoute) Ref() RouteRef { return RouteRef{Scope: "global", RouteID: r.ID} }
func (r GlobalRoute) Valid() bool {
	if !r.HostPolicy.Valid() {
		return false
	}
	return r.Ref().Valid() && namePattern.MatchString(r.Name) && ValidHost(r.Host) && namePattern.MatchString(r.Reservation) && namePattern.MatchString(r.Listener) && (r.DNS == "auto" || r.DNS == "hosts" || r.DNS == "external")
}

type Pin struct {
	ID                 string     `json:"id"`
	Route              RouteRef   `json:"route"`
	ListenerID         string     `json:"listener_id"`
	ListenerGeneration string     `json:"listener_generation"`
	NetworkDigest      string     `json:"network_digest"`
	Port               int        `json:"port"`
	Provider           string     `json:"provider"`
	Service            string     `json:"service,omitempty"`
	HostPolicy         HostPolicy `json:"host_policy,omitempty"`
}

func (p Pin) Valid() bool {
	if !p.HostPolicy.Valid() {
		return false
	}
	return uuidPattern.MatchString(p.ID) && p.Route.Valid() && uuidPattern.MatchString(p.ListenerID) && uuidPattern.MatchString(p.ListenerGeneration) && len(p.NetworkDigest) == 64 && isHex(p.NetworkDigest) && p.Port > 0 && p.Port <= 65535 && (p.Provider == "lan" || p.Provider == "cloudflared" || p.Provider == "tailscale") && (p.Route.Scope == "project" && namePattern.MatchString(p.Service) || p.Route.Scope == "global" && p.Service == "")
}
