package dashboard

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"
)

// ServeDevelopment runs a foreground server for the repository's development runner.
// Its temporary registry is independent of the installed dashboard server.
func ServeDevelopment(ctx context.Context, data, profile, assetDirectory string, out io.Writer) error {
	root, err := os.OpenRoot(assetDirectory)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := root.ReadFile("index.html"); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "devtools-dashboard-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	return serve(ctx, dir, data, root, func(s *Server) error {
		entry := token()
		s.boot[entry] = time.Now().Add(5 * time.Minute)
		_, err := fmt.Fprintf(out, "%s/#%s\n", s.registry.Address, url.Values{"token": {entry}, "profile": {profile}}.Encode())
		return err
	})
}
