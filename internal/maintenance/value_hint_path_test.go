package maintenance

import "testing"

func TestValueHintTransactionPathIsNarrow(t *testing.T) {
	if !validReplacementPath(ValueChangeHintPath) {
		t.Fatal("atomic value hint unsupported")
	}
	for _, path := range []string{".maintenance/arbitrary.json", ".maintenance/../values-change.json", ".maintenance/values-change.json/extra", "/tmp/values-change.json", ".maintenance\\values-change.json"} {
		if validReplacementPath(path) {
			t.Fatal("value hint broadened replacement paths", path)
		}
	}
}
