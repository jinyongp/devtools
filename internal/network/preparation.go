package network

import "github.com/jinyongp/devtools/internal/protocol"

// PreparationMode describes this request's intent, not whether a daemon is running.
type PreparationMode string

const (
	PrepareAuto PreparationMode = "auto"
	PrepareOn   PreparationMode = "on"
	PrepareOff  PreparationMode = "off"
)

func (m PreparationMode) Valid() bool { return m == PrepareAuto || m == PrepareOn || m == PrepareOff }

// PreparationOptions keeps omission distinct from an explicit false value.
func PreparationOptions(proxy, noProxy *bool) (PreparationMode, *protocol.Error) {
	if proxy != nil && noProxy != nil {
		return "", protocol.NewError("invalid_argument", "Specify only one of --proxy and --no-proxy.", 2, map[string]any{"field": "proxy"})
	}
	if proxy != nil {
		if *proxy {
			return PrepareOn, nil
		}
		return PrepareOff, nil
	}
	if noProxy != nil && *noProxy {
		return PrepareOff, nil
	}
	return PrepareAuto, nil
}
