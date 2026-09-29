package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

type benchmarkTransport struct{}

func (benchmarkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func BenchmarkProxyRequestRegisteredProjects(b *testing.B) {
	for _, count := range []int{1, 50, 200} {
		b.Run(fmt.Sprintf("projects_%d", count), func(b *testing.B) {
			root := b.TempDir()
			store := ports.Store{Directory: filepath.Join(root, "ports")}
			err := store.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
				for i := 0; i < count; i++ {
					directory := filepath.Join(root, fmt.Sprintf("project-%d", i))
					if err := os.Mkdir(directory, 0700); err != nil {
						b.Fatal(err)
					}
					config := fmt.Sprintf("profile='bench'\n[ports.web]\n[proxies.http]\nport='web'\nhost='route-%d.localhost'\n", i)
					if err := os.WriteFile(filepath.Join(directory, projectConfigFilename()), []byte(config), 0600); err != nil {
						b.Fatal(err)
					}
					instance := ports.Instance{Profile: "bench", ID: fmt.Sprintf("%032x", i+1), Directory: directory}
					state.Instances = append(state.Instances, instance)
					state.Assignments = append(state.Assignments, ports.Assignment{Instance: instance, Name: "web", Port: 20000 + i})
				}
				return true, nil
			})
			if err != nil {
				b.Fatal(err)
			}
			resolver := NewResolver(store)
			handler := NewHandler(resolver)
			handler.Transport = benchmarkTransport{}
			request := httptest.NewRequest(http.MethodGet, "http://route-0.localhost/file.js", nil)
			if items, err := resolver.List(context.Background(), ""); err != nil || len(items) != count {
				b.Fatalf("warm resolver: %d %+v %v", len(items), items, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				writer := httptest.NewRecorder()
				handler.ServeHTTP(writer, request)
				if writer.Code != http.StatusOK {
					b.Fatalf("response %d: %s", writer.Code, writer.Body.String())
				}
			}
		})
	}
}

func projectConfigFilename() string { return "devtools.toml" }
