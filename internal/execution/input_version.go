package execution

import (
	"strings"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/values"
)

// ValueInputSpec records profile values that contribute to the environment or
// expanded bindings. An injected value overwritten by a port/literal binding
// does not contribute, unless another template actually references it.
func (c Command) ValueInputSpec() values.InputSpec {
	spec := values.InputSpec{Selection: c.Selection, Inject: c.Inject, Bindings: []string{}, Shadowed: []string{}}
	if spec.Selection == "" {
		spec.Selection = "all"
	}
	for name, binding := range c.Bind {
		spec.Shadowed = append(spec.Shadowed, name)
		if binding.Template != nil {
			project.Expand(*binding.Template, func(ref string) (string, bool) {
				if strings.HasPrefix(ref, "var.") {
					spec.Bindings = append(spec.Bindings, strings.TrimPrefix(ref, "var."))
				}
				return "", true
			})
		}
	}
	return spec
}
