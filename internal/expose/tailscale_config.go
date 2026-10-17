package expose

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

type tailscaleConfigAPI interface {
	Read(context.Context) (map[string]json.RawMessage, string, *protocol.Error)
	Write(context.Context, map[string]json.RawMessage, string) *protocol.Error
}
type tailscaleLocalAPI struct {
	client *http.Client
	token  string
}

func (p Tailscale) configAPI(ctx context.Context) (tailscaleConfigAPI, *protocol.Error) {
	if p.API != nil {
		return p.API, nil
	}
	return p.platformConfigAPI(ctx)
}
func (a tailscaleLocalAPI) request(ctx context.Context, method string, body []byte, etag string) ([]byte, string, *protocol.Error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://local-tailscaled.sock/localapi/v0/serve-config", bytes.NewReader(body))
	if err != nil {
		return nil, "", Failure("tailscale_status_invalid")
	}
	if a.token != "" {
		req.SetBasicAuth("", a.token)
	}
	if method == "POST" {
		req.Header.Set("If-Match", etag)
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := a.client.Do(req)
	if err != nil {
		return nil, "", Failure("tailscale_command_failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusPreconditionFailed {
		return nil, "", Failure("tailscale_configuration_changed")
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return nil, "", Failure("tailscale_permission_required")
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", Failure("tailscale_command_failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, "", Failure("tailscale_status_invalid")
	}
	return data, response.Header.Get("ETag"), nil
}
func (a tailscaleLocalAPI) Read(ctx context.Context) (map[string]json.RawMessage, string, *protocol.Error) {
	data, etag, e := a.request(ctx, "GET", nil, "")
	if e != nil {
		return nil, "", e
	}
	if etag == "" || len(etag) > 256 || strings.ContainsAny(etag, "\r\n") {
		return nil, "", Failure("tailscale_atomic_config_unavailable")
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(data, &config) != nil {
		return nil, "", Failure("tailscale_status_invalid")
	}
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	return config, etag, nil
}
func (a tailscaleLocalAPI) Write(ctx context.Context, config map[string]json.RawMessage, etag string) *protocol.Error {
	if etag == "" {
		return Failure("tailscale_atomic_config_unavailable")
	}
	data, err := json.Marshal(config)
	if err != nil {
		return Failure("tailscale_status_invalid")
	}
	_, _, e := a.request(ctx, "POST", data, etag)
	return e
}
func (p Tailscale) readConfig(ctx context.Context) (tailscaleConfig, map[string]json.RawMessage, string, tailscaleConfigAPI, *protocol.Error) {
	api, e := p.configAPI(ctx)
	if e != nil {
		return tailscaleConfig{}, nil, "", nil, e
	}
	raw, etag, e := api.Read(ctx)
	if e != nil {
		return tailscaleConfig{}, nil, "", nil, e
	}
	data, _ := json.Marshal(raw)
	var config tailscaleConfig
	if json.Unmarshal(data, &config) != nil {
		return config, nil, "", nil, Failure("tailscale_status_invalid")
	}
	return config, raw, etag, api, nil
}
func changeServeHandler(raw map[string]json.RawMessage, authority string, port int, origin string, remove bool) *protocol.Error {
	decode := func(data json.RawMessage) (map[string]json.RawMessage, *protocol.Error) {
		out := map[string]json.RawMessage{}
		if len(data) > 0 && string(data) != "null" && json.Unmarshal(data, &out) != nil {
			return nil, Failure("tailscale_status_invalid")
		}
		if out == nil {
			out = map[string]json.RawMessage{}
		}
		return out, nil
	}
	tcp, e := decode(raw["TCP"])
	if e != nil {
		return e
	}
	web, e := decode(raw["Web"])
	if e != nil {
		return e
	}
	server, e := decode(web[authority])
	if e != nil {
		return e
	}
	handlers, e := decode(server["Handlers"])
	if e != nil {
		return e
	}
	portKey := strconv.Itoa(port)
	if remove {
		if !ownedHandler(handlers["/"], origin) {
			return Failure("tailscale_ownership_changed")
		}
		delete(handlers, "/")
		if len(handlers) == 0 {
			delete(web, authority)
			shared := false
			for host := range web {
				if strings.HasSuffix(host, ":"+portKey) {
					shared = true
				}
			}
			if !shared {
				if current := tcp[portKey]; len(current) > 0 {
					fields, err := decode(current)
					if err != nil {
						return err
					}
					var https bool
					if len(fields) != 1 || json.Unmarshal(fields["HTTPS"], &https) != nil || !https {
						return Failure("tailscale_ownership_changed")
					}
				}
				delete(tcp, portKey)
			}
		} else {
			server["Handlers"], _ = json.Marshal(handlers)
			web[authority], _ = json.Marshal(server)
		}
	} else {
		if len(tcp[portKey]) > 0 || len(web[authority]) > 0 {
			return Failure("tailscale_port_conflict")
		}
		tcp[portKey] = json.RawMessage(`{"HTTPS":true}`)
		handler, _ := json.Marshal(map[string]string{"Proxy": origin})
		server["Handlers"], _ = json.Marshal(map[string]json.RawMessage{"/": handler})
		web[authority], _ = json.Marshal(server)
	}
	raw["TCP"], _ = json.Marshal(tcp)
	raw["Web"], _ = json.Marshal(web)
	return nil
}
