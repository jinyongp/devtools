// Package dashboard serves the embedded, read-only task graph on loopback.
package dashboard

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

//go:embed assets/*
var assets embed.FS

type Registry struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Token   string `json:"token"`
}

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func fail() *protocol.Error {
	return protocol.NewError("dashboard_error", "Cannot manage the local dashboard server.", 1, nil)
}
func admin(ctx context.Context, r Registry, action string) (tasks.Object, error) {
	body, _ := json.Marshal(tasks.Object{"action": action})
	req, e := http.NewRequestWithContext(ctx, "POST", r.Address+"/admin", bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	client := http.Client{Timeout: time.Second}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("dashboard unavailable")
	}
	var o tasks.Object
	e = json.NewDecoder(res.Body).Decode(&o)
	return o, e
}
func Manage(ctx context.Context, action, cache, data, profile string) (tasks.Object, *protocol.Error) {
	dir := filepath.Join(cache, "dashboard")
	unlock, e := tasks.Lock(ctx, filepath.Join(dir, "start.lock"))
	if e != nil {
		return nil, fail()
	}
	defer unlock()
	path := filepath.Join(dir, "server.json")
	r := Registry{}
	_ = tasks.ReadPrivate(path, &r)
	status, e := admin(ctx, r, "status")
	running := e == nil && status["server_id"] == r.ID
	if action == "status" {
		if running {
			return tasks.Object{"running": true, "server_id": r.ID}, nil
		}
		return tasks.Object{"running": false, "server_id": nil}, nil
	}
	if action == "stop" {
		if !running {
			return tasks.Object{"stopped": false}, nil
		}
		if _, e = admin(ctx, r, "stop"); e != nil {
			return nil, fail()
		}
		return tasks.Object{"stopped": true}, nil
	}
	if !running {
		exe, e := os.Executable()
		if e != nil {
			return nil, fail()
		}
		cmd := exec.Command(exe, "__dashboard-serve", dir, data)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if e = cmd.Start(); e != nil {
			return nil, fail()
		}
		go func() { _ = cmd.Wait() }()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return nil, fail()
			case <-time.After(50 * time.Millisecond):
			}
			if tasks.ReadPrivate(path, &r) == nil {
				if _, e = admin(ctx, r, "status"); e == nil {
					running = true
					break
				}
			}
		}
		if !running {
			return nil, fail()
		}
	}
	link, e := admin(ctx, r, "link")
	if e != nil {
		return nil, fail()
	}
	return tasks.Object{"running": true, "server_id": r.ID, "initial_profile": profile, "url": r.Address + "/#token=" + link["token"].(string) + "&profile=" + profile}, nil
}

type Server struct {
	mu       sync.Mutex
	registry Registry
	data     string
	cache    string
	boot     map[string]time.Time
	sessions map[string]time.Time
	stop     func()
	results  map[string]cachedQuery
}
type cachedQuery struct {
	Stamp   string
	Expires time.Time
	Body    []byte
}

func Serve(ctx context.Context, dir, data string) error {
	lockCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	unlock, e := tasks.Lock(lockCtx, filepath.Join(dir, "serve.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return e
	}
	defer listener.Close()
	r := Registry{ID: tasks.ID(), Address: "http://" + listener.Addr().String(), Token: token()}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	s := &Server{registry: r, data: data, cache: filepath.Join(dir, "queries"), boot: map[string]time.Time{}, sessions: map[string]time.Time{}}
	s.stop = func() {
		go func() {
			ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
			defer c()
			_ = server.Shutdown(ctx)
		}()
	}
	server.Handler = s
	if e = tasks.WritePrivate(filepath.Join(dir, "server.json"), r); e != nil {
		return e
	}
	defer os.Remove(filepath.Join(dir, "server.json"))
	go func() { <-ctx.Done(); s.stop() }()
	e = server.Serve(listener)
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := s.registry.Address
	if r.Host != strings.TrimPrefix(origin, "http://") || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) {
		http.Error(w, "Forbidden", 403)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
	reply := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	if r.URL.Path == "/admin" {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+s.registry.Token {
			http.Error(w, "Forbidden", 403)
			return
		}
		var v struct {
			Action string `json:"action"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&v) != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		switch v.Action {
		case "status":
			reply(tasks.Object{"server_id": s.registry.ID})
		case "link":
			s.mu.Lock()
			now := time.Now()
			for k, t := range s.boot {
				if now.After(t) {
					delete(s.boot, k)
				}
			}
			k := token()
			s.boot[k] = now.Add(5 * time.Minute)
			s.mu.Unlock()
			reply(tasks.Object{"token": k})
		case "stop":
			reply(tasks.Object{"stopped": true})
			s.stop()
		default:
			http.Error(w, "Invalid action", 400)
		}
		return
	}
	if r.URL.Path == "/session" {
		if r.Method != "POST" || r.Header.Get("Origin") != origin {
			http.Error(w, "Forbidden", 403)
			return
		}
		var v struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&v) != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		s.mu.Lock()
		expires, ok := s.boot[v.Token]
		delete(s.boot, v.Token)
		if !ok || time.Now().After(expires) {
			s.mu.Unlock()
			http.Error(w, "Link expired. Run devtools dashboard for a new link.", 401)
			return
		}
		k := token()
		s.sessions[k] = time.Now().Add(8 * time.Hour)
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "devtools_session", Value: k, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		reply(tasks.Object{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" {
			http.Error(w, "Read-only API", 405)
			return
		}
		c, e := r.Cookie("devtools_session")
		s.mu.Lock()
		valid := e == nil && time.Now().Before(s.sessions[cookieValue(c)])
		if valid {
			s.sessions[c.Value] = time.Now().Add(8 * time.Hour)
		}
		for k, t := range s.sessions {
			if time.Now().After(t) {
				delete(s.sessions, k)
			}
		}
		s.mu.Unlock()
		if !valid {
			http.Error(w, "Session expired. Run devtools dashboard for a new link.", 401)
			return
		}
		store := tasks.Store{Directory: s.data, Profile: r.URL.Query().Get("profile"), Cache: s.cache}
		if r.URL.Path == "/api/profiles" {
			profiles, e := store.Profiles()
			if e != nil {
				http.Error(w, e.Message, 500)
				return
			}
			reply(tasks.Object{"profiles": profiles})
			return
		}
		if r.URL.Path != "/api/query" {
			http.NotFound(w, r)
			return
		}
		command := r.URL.Query().Get("command")
		allowed := map[string]bool{"workstream list": true, "workstream tree": true, "workstream context": true, "list": true, "tree": true, "context": true, "history": true, "validation show": true}
		if !allowed[command] {
			http.Error(w, "Unknown query", 400)
			return
		}
		opts := map[string]string{}
		for _, k := range []string{"workstream", "cursor", "limit", "depth", "direction", "state"} {
			opts[k] = r.URL.Query().Get(k)
		}
		key := r.URL.Query().Encode()
		signature := ""
		if info, e := os.Stat(filepath.Join(s.data, hex.EncodeToString([]byte(store.Profile))+".json")); e == nil {
			signature = fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
		}
		s.mu.Lock()
		cached, found := s.results[key]
		if found && opts["cursor"] == "" && signature != "" && cached.Stamp == signature && time.Now().Before(cached.Expires) {
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cached.Body)
			return
		}
		s.mu.Unlock()
		result, err := store.Query(tasks.Query{Command: command, Target: r.URL.Query().Get("id"), Options: opts})
		if err != nil {
			w.WriteHeader(400)
			reply(tasks.Object{"error": err})
			return
		}
		if opts["cursor"] != "" {
			reply(result)
			return
		}
		body, _ := json.Marshal(result)
		s.mu.Lock()
		if s.results == nil {
			s.results = map[string]cachedQuery{}
		}
		now := time.Now()
		for k, v := range s.results {
			if now.After(v.Expires) {
				delete(s.results, k)
			}
		}
		if len(s.results) >= 128 {
			s.results = map[string]cachedQuery{}
		}
		s.results[key] = cachedQuery{Stamp: signature, Expires: now.Add(30 * time.Minute), Body: body}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	b, e := assets.ReadFile("assets/" + name)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	switch filepath.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "text/javascript")
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, _ = w.Write(b)
}
func cookieValue(c *http.Cookie) string {
	if c == nil {
		return ""
	}
	return c.Value
}
