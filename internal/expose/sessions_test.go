package expose

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRevocationPreservesOtherHTTP2Streams(t *testing.T) {
	var first, second Sessions
	alive := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := &first
		if r.URL.Path == "/second" {
			s = &second
		}
		w, r, end, ok := s.Begin(w, r)
		if !ok {
			http.Error(w, "closed", 503)
			return
		}
		defer end()
		fmt.Fprintln(w, "opened")
		w.(http.Flusher).Flush()
		if r.URL.Path == "/second" {
			select {
			case <-alive:
				fmt.Fprintln(w, "alive")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
		<-r.Context().Done()
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	a, e := client.Get(server.URL + "/first")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Body.Close()
	b, e := client.Get(server.URL + "/second")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Body.Close()
	if a.ProtoMajor != 2 || b.ProtoMajor != 2 {
		t.Fatal("HTTP/2 not exercised")
	}
	ra, rb := bufio.NewReader(a.Body), bufio.NewReader(b.Body)
	if line, e := ra.ReadString('\n'); e != nil || line != "opened\n" {
		t.Fatal(line, e)
	}
	if line, e := rb.ReadString('\n'); e != nil || line != "opened\n" {
		t.Fatal(line, e)
	}
	if !first.Close(context.Background()) {
		t.Fatal("target stream did not finish")
	}
	if line, e := ra.ReadString('\n'); e == nil || line != "" {
		t.Fatal("revoked stream continued", line, e)
	}
	close(alive)
	if line, e := rb.ReadString('\n'); e != nil || line != "alive\n" {
		t.Fatal("other stream canceled", line, e)
	}
	if !second.Close(context.Background()) {
		t.Fatal("second stream did not finish")
	}
}

func TestRevocationClosesOnlyOwnedHijackedConnections(t *testing.T) {
	var first, second Sessions
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := &first
		if r.URL.Path == "/second" {
			s = &second
		}
		w, _, end, ok := s.Begin(w, r)
		if !ok {
			http.Error(w, "closed", 503)
			return
		}
		defer end()
		conn, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		_, _ = io.Copy(conn, conn)
	}))
	defer server.Close()
	connect := func(path string) net.Conn {
		t.Helper()
		conn, e := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
		if e != nil {
			t.Fatal(e)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: example.local\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", path)
		reader := bufio.NewReader(conn)
		response, e := http.ReadResponse(reader, &http.Request{Method: "GET"})
		if e != nil || response.StatusCode != 101 {
			t.Fatal(response, e)
		}
		return conn
	}
	a, b := connect("/first"), connect("/second")
	defer a.Close()
	defer b.Close()
	if !first.Close(context.Background()) {
		t.Fatal("hijacked stream did not close")
	}
	if _, e := a.Read(make([]byte, 1)); e == nil {
		t.Fatal("revoked connection readable")
	}
	if _, e := b.Write([]byte("x")); e != nil {
		t.Fatal("other connection closed", e)
	}
	data := make([]byte, 1)
	if _, e := io.ReadFull(b, data); e != nil || string(data) != "x" {
		t.Fatal("other connection interrupted", e)
	}
	if !second.Close(context.Background()) {
		t.Fatal("second stream did not close")
	}
}
