package execution

import (
	"reflect"
	"sort"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
)

func TestValueInputSpecTracksOnlyExpandedProfileBindings(t *testing.T) {
	template := "${var.URL}/$${var.ESCAPED}/${var.URL}"
	literal := "literal"
	command := Command{Selection: "frontend", Inject: false, Bind: map[string]project.Binding{"ADDRESS": {Template: &template}, "URL": {Template: &literal}}}
	spec := command.ValueInputSpec()
	sort.Strings(spec.Shadowed)
	if spec.Selection != "frontend" || spec.Inject || !reflect.DeepEqual(spec.Bindings, []string{"URL", "URL"}) || !reflect.DeepEqual(spec.Shadowed, []string{"ADDRESS", "URL"}) {
		t.Fatal("wrong delivered dependency spec", spec)
	}
	if !spec.UsesValues() {
		t.Fatal("non-injected template lost dependency")
	}
	if (Command{}).ValueInputSpec().UsesValues() {
		t.Fatal("plain command invented profile dependency")
	}
}
