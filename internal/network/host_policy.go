package network

// HostPolicy changes the HTTP authority, never the backend dial target.
// The empty representation preserves legacy storage and identity digests.
type HostPolicy string

const (
	HostRewrite  HostPolicy = "rewrite"
	HostPreserve HostPolicy = "preserve"
)

func (p HostPolicy) Valid() bool { return p == "" || p == HostRewrite || p == HostPreserve }

func (p HostPolicy) Canonical() HostPolicy {
	if p == HostRewrite {
		return ""
	}
	return p
}

func (p HostPolicy) Effective() HostPolicy {
	if p == "" {
		return HostRewrite
	}
	return p
}
