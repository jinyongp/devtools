package values

import "strings"

var frontendPrefixes = []string{"VITE_", "NEXT_PUBLIC_", "REACT_APP_", "NUXT_PUBLIC_", "PUBLIC_"}

type Warning struct {
	Code    string `json:"code"`
	Key     string `json:"key"`
	Message string `json:"message"`
}

func FrontendPublicPrefixes() []string { return append([]string{}, frontendPrefixes...) }

// A name is a hint, not proof of browser exposure. Storing it as a secret cannot
// override a build tool which deliberately publishes that environment prefix.
func FrontendSecretWarning(key string) *Warning {
	upper := strings.ToUpper(key)
	for _, prefix := range frontendPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return &Warning{Code: "frontend_public_prefix", Key: key, Message: "This name is commonly published by frontend builds. Keep credentials out of browser bundles."}
		}
	}
	return nil
}
func ImportWarnings(items []ImportItem) []Warning {
	var warnings []Warning
	for _, item := range items {
		if item.Kind == Secret {
			if warning := FrontendSecretWarning(item.Key); warning != nil {
				warnings = append(warnings, *warning)
			}
		}
	}
	return warnings
}
