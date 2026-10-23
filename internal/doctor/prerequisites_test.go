package doctor

import (
	"context"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/values"
	"testing"
)

func TestRequirementConditionsAvoidInvalidRegistration(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		p            values.Prerequisite
		known        bool
	}{
		{"wrong kind", "kind_mismatch", values.Prerequisite{Kind: values.Variable, Selected: true, HasSlot: true}, true},
		{"excluded", "outside_bundle", values.Prerequisite{Kind: values.Secret, HasSlot: true}, true},
		{"other env", "slot_missing", values.Prerequisite{Kind: values.Secret, Selected: true}, true},
		{"expired", "value_expired", values.Prerequisite{Kind: values.Secret, Selected: true, HasSlot: true, Expired: true}, true},
		{"unverified", "registration_unverified", values.Prerequisite{Kind: values.Secret}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := CheckRequirements(context.Background(), Input{Profile: "p", Env: "local", Inject: true, RegistrationKnown: tc.known, Requirements: project.Requirements{Secs: []string{"TOKEN"}}, Prerequisites: map[string]values.Prerequisite{"TOKEN": tc.p}})
			c := checks[0]
			if c.Reason != tc.reason {
				t.Fatal(c)
			}
			if tc.reason == "kind_mismatch" || tc.reason == "outside_bundle" {
				for _, r := range c.Remedies {
					if len(r.Argv) > 0 {
						t.Fatal("invalid automatic registration", c)
					}
				}
			}
		})
	}
}
