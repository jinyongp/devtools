package values

import (
	"encoding/json"
	"testing"
)

func TestImportAtomicityAndLayers(t *testing.T) {
	s := newState("app")
	s.CreateEnv("local")
	s.Set(Variable, "PORT", "", "3000")
	s.Set(Secret, "TOKEN", "", "old")
	before, _ := json.Marshal(s)
	_, _, err := s.Import(map[string]string{"NEW": "new", "TOKEN": "different"}, nil, "", false)
	after, _ := json.Marshal(s)
	if err == nil || err.Code != "import_conflict" || string(before) != string(after) {
		t.Fatal("conflict changed state")
	}
	items, changed, err := s.Import(map[string]string{"PORT": "4000", "TOKEN": "new", "EMPTY": ""}, nil, "local", false)
	if err != nil || !changed || len(items) != 3 || s.Keys["PORT"].Kind != Variable || *s.Keys["PORT"].Common != "3000" {
		t.Fatal("layer import failed")
	}
	_, changed, err = s.Import(map[string]string{"PORT": "4000", "TOKEN": "new", "EMPTY": ""}, nil, "local", false)
	if err != nil || changed {
		t.Fatal("repeat import must be unchanged")
	}
	_, _, err = s.Import(map[string]string{"TOKEN": "new"}, []string{"TOKEN"}, "local", true)
	if err == nil || s.Keys["TOKEN"].Kind != Secret {
		t.Fatal("secret kind changed")
	}
	_, _, err = s.Import(map[string]string{"PORT": "5000"}, nil, "local", true)
	if err != nil || s.Keys["PORT"].Envs["local"] != "5000" {
		t.Fatal("overwrite failed")
	}
	s.Unset(Variable, "PORT", "")
	s.Unset(Variable, "PORT", "local")
	_, _, err = s.Import(map[string]string{"PORT": "6000"}, nil, "", false)
	if err != nil || s.Keys["PORT"].Kind != Variable {
		t.Fatal("removed key kind was lost")
	}
}
