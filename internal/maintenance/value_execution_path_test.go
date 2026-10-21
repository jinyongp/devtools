package maintenance

import "testing"

func TestValueExecutionTransactionPathsAreNarrow(t *testing.T) {
	profile := "p1-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	id := "0123456789abcdef0123456789abcdef"
	uuid := "01234567-89ab-4cde-8fab-0123456789ab"
	for _, domain := range []string{"value-executions", "value-execution-payloads"} {
		if !validReplacementPath(domain + "/" + profile + "/" + id + ".json") {
			t.Fatal("expected execution target rejected")
		}
	}
	for _, path := range []string{"value-execution-requests/" + profile + "/" + uuid + ".json", "value-runners/" + uuid + ".json", "value-runner-requests/" + uuid + ".json"} {
		if !validReplacementPath(path) {
			t.Fatal("expected request target rejected")
		}
	}
	for _, path := range []string{"value-executions/" + id + ".json", "value-executions/" + profile + "/../" + id + ".json", "value-executions/" + profile + "/0123456789ABCDEF0123456789abcdef.json", "value-executions/" + profile + "/" + id + ".json/extra", "value-execution-locks/" + profile + "/" + id + ".lock", "value-runners/01234567-89AB-4cde-8fab-0123456789ab.json", "value-runner-transports/" + uuid + "/session.sock", "value-execution-requests/" + profile + "/" + id + ".json"} {
		if validReplacementPath(path) {
			t.Fatal("unsafe or unplanned target admitted", path)
		}
	}
}
