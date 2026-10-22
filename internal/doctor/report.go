package doctor

type CommandReport struct {
	Name      string  `json:"name"`
	Profile   *string `json:"profile"`
	Env       *string `json:"env"`
	Selection *string `json:"selection"`
	State     string  `json:"state"`
	Checks    []Check `json:"checks"`
}
type Summary struct {
	Commands       int `json:"commands"`
	Ready          int `json:"ready"`
	NotReady       int `json:"not_ready"`
	Unknown        int `json:"unknown"`
	RequiredChecks int `json:"required_checks"`
	Passed         int `json:"passed"`
	Failed         int `json:"failed"`
	UnknownChecks  int `json:"unknown_checks"`
}
type Diagnosis struct {
	Scope      string          `json:"scope"`
	Mode       string          `json:"mode"`
	ConfigPath *string         `json:"config_path"`
	Directory  string          `json:"directory"`
	Profile    *string         `json:"profile"`
	State      string          `json:"state"`
	Ready      bool            `json:"ready"`
	Selection  []string        `json:"selection"`
	Checks     []Check         `json:"checks"`
	Commands   []CommandReport `json:"commands"`
	Summary    Summary         `json:"summary"`
}

func (r *Diagnosis) Finalize() {
	r.Summary = Summary{Commands: len(r.Commands)}
	all := append([]Check{}, r.Checks...)
	count := func(checks []Check) {
		for _, c := range checks {
			if !c.Required {
				continue
			}
			r.Summary.RequiredChecks++
			switch c.Status {
			case "pass":
				r.Summary.Passed++
			case "fail":
				r.Summary.Failed++
			case "unknown":
				r.Summary.UnknownChecks++
			}
		}
	}
	count(r.Checks)
	for i := range r.Commands {
		c := &r.Commands[i]
		c.State = State(append(append([]Check{}, r.Checks...), c.Checks...))
		switch c.State {
		case "ready":
			r.Summary.Ready++
		case "not_ready":
			r.Summary.NotReady++
		default:
			r.Summary.Unknown++
		}
		count(c.Checks)
		all = append(all, c.Checks...)
	}
	r.State = State(all)
	r.Ready = r.State == "ready"
}
