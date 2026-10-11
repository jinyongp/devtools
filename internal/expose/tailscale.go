package expose

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Tailscale struct {
	Data, Executable string
	Run              func(context.Context, []string) ([]byte, *protocol.Error)
	API              tailscaleConfigAPI
}
type tailscaleOwner struct {
	Authority, Origin string
	Port              int
	Generation        string
}
type tailscaleConfig struct {
	TCP         map[string]json.RawMessage
	Web         map[string]struct{ Handlers map[string]json.RawMessage }
	AllowFunnel map[string]bool
	Foreground  map[string]json.RawMessage
}
type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	if available := b.limit - b.Len(); available > 0 {
		if len(data) > available {
			data = data[:available]
		}
		_, _ = b.Buffer.Write(data)
	}
	return n, nil
}
func (p Tailscale) run(ctx context.Context, args ...string) ([]byte, *protocol.Error) {
	if p.Run != nil {
		return p.Run(ctx, args)
	}
	path := p.Executable
	if path == "" {
		var e error
		path, e = exec.LookPath("tailscale")
		if e != nil {
			return nil, Failure("tailscale_not_installed")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	out := &boundedOutput{limit: 1 << 20}
	diagnostic := &boundedOutput{limit: 8192}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	cmd.WaitDelay = time.Second
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return nil, Failure("tailscale_timeout")
		}
		return nil, Failure("tailscale_command_failed")
	}
	if out.Len() == 1<<20 {
		return nil, Failure("tailscale_status_invalid")
	}
	return out.Bytes(), nil
}
func (p Tailscale) config(ctx context.Context) (tailscaleConfig, *protocol.Error) {
	config, _, _, _, e := p.readConfig(ctx)
	return config, e
}
func (p Tailscale) node(ctx context.Context) (string, *protocol.Error) {
	data, e := p.run(ctx, "status", "--json")
	if e != nil {
		return "", e
	}
	var status struct {
		Version, BackendState string
		Self                  *struct{ DNSName string }
		CertDomains           []string
	}
	if json.Unmarshal(data, &status) != nil {
		return "", Failure("tailscale_status_invalid")
	}
	version := strings.Split(status.Version, ".")
	if len(version) < 2 {
		return "", Failure("tailscale_version_unsupported")
	}
	major, _ := strconv.Atoi(version[0])
	minor, _ := strconv.Atoi(version[1])
	if major < 1 || major == 1 && minor < 52 {
		return "", Failure("tailscale_version_unsupported")
	}
	if status.BackendState != "Running" {
		return "", Failure("tailscale_not_logged_in")
	}
	if status.Self == nil {
		return "", Failure("tailscale_dns_required")
	}
	host := strings.TrimSuffix(status.Self.DNSName, ".")
	if !network.ValidHost(host) || !strings.HasSuffix(host, ".ts.net") {
		return "", Failure("tailscale_dns_required")
	}
	cert := false
	for _, domain := range status.CertDomains {
		cert = cert || strings.TrimSuffix(domain, ".") == host
	}
	if !cert {
		return "", Failure("tailscale_https_required")
	}
	return host, nil
}
func ownedHandler(raw json.RawMessage, origin string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 {
		return false
	}
	var value string
	return json.Unmarshal(fields["Proxy"], &value) == nil && strings.TrimSuffix(value, "/") == origin
}
func (p Tailscale) Start(ctx context.Context, r Record, origin string) (ProviderSession, *protocol.Error) {
	if !ValidOrigin(origin) || r.ServePort < 1 || r.ServePort > 65535 {
		return nil, Failure("invalid_argument")
	}
	host, e := p.node(ctx)
	if e != nil {
		return nil, e
	}
	config, raw, etag, api, e := p.readConfig(ctx)
	if e != nil {
		return nil, e
	}
	authority := net.JoinHostPort(host, strconv.Itoa(r.ServePort))
	owner := tailscaleOwner{Authority: authority, Origin: origin, Port: r.ServePort, Generation: r.Generation}
	if config.AllowFunnel[authority] {
		return nil, Failure("tailscale_funnel_conflict")
	}
	if len(config.Foreground) > 0 {
		return nil, Failure("tailscale_foreground_conflict")
	}
	_, portExists := config.TCP[strconv.Itoa(r.ServePort)]
	_, webExists := config.Web[authority]
	if portExists || webExists {
		return nil, Failure("tailscale_port_conflict")
	}
	encoded, _ := json.Marshal(owner)
	r.ProviderOwner = string(encoded)
	r.URL = "https://" + host
	if r.ServePort != 443 {
		r.URL = "https://" + authority
	}
	if p.Data != "" {
		if e := (Store{Data: p.Data}).Write(r); e != nil {
			return nil, e
		}
	}
	if e := changeServeHandler(raw, authority, r.ServePort, origin, false); e != nil {
		return nil, e
	}
	if e := api.Write(ctx, raw, etag); e != nil {
		return nil, e
	}
	s := &tailscaleSession{provider: p, record: r, owner: owner}
	if !s.Alive(ctx) {
		return nil, Failure("tailscale_commit_unconfirmed")
	}
	return s, nil
}

type tailscaleSession struct {
	provider Tailscale
	record   Record
	owner    tailscaleOwner
}

func (s *tailscaleSession) URL() string   { return s.record.URL }
func (s *tailscaleSession) Owner() string { return s.record.ProviderOwner }
func (s *tailscaleSession) Alive(ctx context.Context) bool {
	c, e := s.provider.config(ctx)
	if e != nil || c.AllowFunnel[s.owner.Authority] {
		return false
	}
	var tcp struct {
		HTTPS      bool
		TCPForward string
		HTTP       bool
	}
	if json.Unmarshal(c.TCP[strconv.Itoa(s.owner.Port)], &tcp) != nil || !tcp.HTTPS || tcp.TCPForward != "" || tcp.HTTP {
		return false
	}
	return ownedHandler(c.Web[s.owner.Authority].Handlers["/"], s.owner.Origin)
}
func (s *tailscaleSession) Stop(ctx context.Context) *protocol.Error {
	c, raw, etag, api, e := s.provider.readConfig(ctx)
	if e != nil {
		return e
	}
	handler := c.Web[s.owner.Authority].Handlers["/"]
	if len(handler) == 0 {
		return nil
	}
	if !ownedHandler(handler, s.owner.Origin) {
		return Failure("tailscale_ownership_changed")
	}
	if e := changeServeHandler(raw, s.owner.Authority, s.owner.Port, s.owner.Origin, true); e != nil {
		return e
	}
	if e := api.Write(ctx, raw, etag); e != nil {
		return e
	}
	c, e = s.provider.config(ctx)
	if e != nil {
		return e
	}
	if len(c.Web[s.owner.Authority].Handlers["/"]) != 0 {
		return Failure("tailscale_stop_unconfirmed")
	}
	return nil
}
func (p Tailscale) RecoverStop(ctx context.Context, r Record) *protocol.Error {
	var owner tailscaleOwner
	if json.Unmarshal([]byte(r.ProviderOwner), &owner) != nil || owner.Generation != r.Generation || owner.Port != r.ServePort || owner.Origin != network.BackendURL(r.OriginPort) || !ValidOrigin(owner.Origin) {
		return Failure("tailscale_ownership_invalid")
	}
	return (&tailscaleSession{provider: p, record: r, owner: owner}).Stop(ctx)
}
