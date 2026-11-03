package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestPortCollectionsScopeIntersectionsAndExplicitAllProfiles(t *testing.T) {
	a := testApp(t)
	dirs := []string{privateTempDir(t), privateTempDir(t), privateTempDir(t)}
	for i, dir := range dirs {
		profile := "app-a"
		if i == 2 {
			profile = "app-b"
		}
		if e := os.WriteFile(filepath.Join(dir, "devtools.toml"), []byte("profile='"+profile+"'\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	store, e := a.portStore()
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	if e := store.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		for i, dir := range dirs {
			profile := "app-a"
			if i == 2 {
				profile = "app-b"
			}
			instance, e := st.Register(profile, dir)
			if e != nil {
				return false, e
			}
			ids = append(ids, instance.ID)
			st.Assignments = append(st.Assignments, ports.Assignment{Instance: *instance, Name: "web", Port: 3000 + i})
		}
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	t.Chdir(dirs[0])
	for _, tc := range []struct {
		args  []string
		count int
	}{
		{[]string{"instance", "list"}, 1},
		{[]string{"instance", "list", "--profile", "app-a"}, 2},
		{[]string{"instance", "list", "--all-profiles"}, 3},
		{[]string{"instance", "list", "--dir", dirs[0], "--instance", "id:" + ids[0]}, 1},
		{[]string{"port", "list", "--all-profiles", "--service", "web", "--protocol", "tcp"}, 3},
	} {
		if got := outputData(t, a, tc.args)["items"].([]any); len(got) != tc.count {
			t.Fatal(tc.args, got)
		}
	}
	for _, args := range [][]string{{"instance", "list", "--profile", "app-b", "--dir", dirs[0]}, {"instance", "list", "--dir", dirs[0], "--instance", "id:" + ids[1]}} {
		if code, _, err := invoke(t, a, "", args...); code != 2 {
			t.Fatal(args, code, err)
		}
	}
	// Explicit profile spans locations even from a different project's cwd.
	t.Chdir(dirs[2])
	if rows := outputData(t, a, []string{"port", "list", "--profile", "app-a"})["items"].([]any); len(rows) != 2 {
		t.Fatal(rows)
	}
}
