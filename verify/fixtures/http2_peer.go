// A standard-library peer for the installed CLI's actual TLS listener.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"time"
)

func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}

func main() {
	require(len(os.Args) == 3, "expected CA file and listener port")
	pem, err := os.ReadFile(os.Args[1])
	require(err == nil, "cannot read public CA")
	roots := x509.NewCertPool()
	require(roots.AppendCertsFromPEM(pem), "invalid public CA")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	transport := &http.Transport{
		Proxy: nil, ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", os.Args[2]))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request := func(host, path, authority string) (*http.Response, net.Conn) {
		var connection net.Conn
		trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { connection = info.Conn }}
		req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "GET", "https://"+host+":"+os.Args[2]+path, nil)
		require(err == nil, "cannot construct request")
		if authority != "" {
			req.Host = authority
		}
		res, err := client.Do(req)
		require(err == nil, fmt.Sprintf("HTTP/2 request failed: %v", err))
		require(res.ProtoMajor == 2, "actual listener did not negotiate HTTP/2")
		return res, connection
	}
	type stream struct {
		response *http.Response
		reader   *bufio.Reader
		conn     net.Conn
	}
	open := func(host string) stream {
		res, conn := request(host, "/stream", "")
		require(res.StatusCode == 200, "stream request rejected")
		reader := bufio.NewReader(res.Body)
		line, err := reader.ReadString('\n')
		require(err == nil && strings.HasPrefix(line, "alive "), "stream did not become ready")
		return stream{res, reader, conn}
	}
	first, second := open("app.local"), open("app.local")
	defer first.response.Body.Close()
	defer second.response.Body.Close()
	require(first.conn == second.conn, "target streams did not share one HTTP/2 connection")
	sibling := open("app.localhost")
	defer sibling.response.Body.Close()
	mismatch, _ := request("app.local", "/", "app.localhost:"+os.Args[2])
	require(mismatch.StatusCode >= 400 && mismatch.StatusCode < 500, "HTTP/2 bypassed SNI/authority validation")
	mismatch.Body.Close()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": true, "http2": true, "multiplexed": true})
	_, err = bufio.NewReader(os.Stdin).ReadString('\n')
	require(err == nil, "controller did not confirm selected stop")
	for _, target := range []stream{first, second} {
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, target.reader)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			panic("selected HTTP/2 stream survived stop")
		}
	}
	// Require a fresh post-stop heartbeat, rather than a buffered pre-stop line.
	cutoff := time.Now().UnixNano()
	for {
		line, err := sibling.reader.ReadString('\n')
		require(err == nil, "local sibling stream was revoked")
		var heartbeat int64
		_, err = fmt.Sscanf(line, "alive %d", &heartbeat)
		require(err == nil, "invalid sibling heartbeat")
		if heartbeat > cutoff {
			break
		}
	}
	res, _ := request("app.localhost", "/", "")
	require(res.StatusCode == 200, "local route failed after selected stop")
	res.Body.Close()
	json.NewEncoder(os.Stdout).Encode(map[string]bool{"revoked": true, "sibling_preserved": true, "authority_checked": true})
}
