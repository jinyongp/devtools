package redact

import "errors"

type Mode string

const (
	Auto     Mode = "auto"
	Redacted Mode = "redacted"
	Raw      Mode = "raw"
)

type Policy struct {
	Mode   Mode `json:"mode"`
	Stdout Mode `json:"stdout,omitempty"`
	Stderr Mode `json:"stderr,omitempty"`
}
type Plan struct {
	Requested    Policy `json:"requested"`
	Stdout       Mode   `json:"stdout"`
	Stderr       Mode   `json:"stderr"`
	StdoutReason string `json:"stdout_reason"`
	StderrReason string `json:"stderr_reason"`
	ShortSecrets bool   `json:"short_secret_patterns"`
}

func validMode(mode Mode) bool { return mode == "" || mode == Auto || mode == Redacted || mode == Raw }
func (p Policy) Valid() bool   { return validMode(p.Mode) && validMode(p.Stdout) && validMode(p.Stderr) }
func (p Policy) Normalize() Policy {
	if p.Mode == "" {
		p.Mode = Auto
	}
	return p
}
func Resolve(policy Policy, stdoutTTY, stderrTTY, capture bool) (Plan, error) {
	if !policy.Valid() {
		return Plan{}, errors.New("invalid_output_mode")
	}
	policy = policy.Normalize()
	resolve := func(override Mode, tty bool) (Mode, string) {
		if capture {
			return Redacted, "capture"
		}
		mode := policy.Mode
		if override != "" {
			mode = override
		}
		if mode != Auto {
			return mode, "explicit"
		}
		if tty {
			return Raw, "tty"
		}
		return Redacted, "non_tty"
	}
	out, outReason := resolve(policy.Stdout, stdoutTTY)
	diagnostic, errReason := resolve(policy.Stderr, stderrTTY)
	return Plan{Requested: policy, Stdout: out, Stderr: diagnostic, StdoutReason: outReason, StderrReason: errReason}, nil
}

func (p Plan) Valid() bool {
	if !p.Requested.Valid() || p.Requested.Mode == "" {
		return false
	}
	if p.StdoutReason == "capture" || p.StderrReason == "capture" {
		return p.StdoutReason == "capture" && p.StderrReason == "capture" && p.Stdout == Redacted && p.Stderr == Redacted
	}
	valid := func(override Mode, effective Mode, reason string) bool {
		requested := p.Requested.Mode
		if override != "" {
			requested = override
		}
		switch reason {
		case "explicit":
			return requested != Auto && effective == requested
		case "tty":
			return requested == Auto && effective == Raw
		case "non_tty":
			return requested == Auto && effective == Redacted
		default:
			return false
		}
	}
	return valid(p.Requested.Stdout, p.Stdout, p.StdoutReason) && valid(p.Requested.Stderr, p.Stderr, p.StderrReason)
}
