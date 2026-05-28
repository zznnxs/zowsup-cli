package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

// newConnectDialer returns a Dialer that opens a stream to (host, port) via
// an HTTP/HTTPS CONNECT proxy. Authentication is sent only when credentials
// are non-empty.
func newConnectDialer(cfg Config) Dialer {
	proxyAddr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	useTLS := cfg.Scheme == "https"
	return func(network, address string) (net.Conn, error) {
		base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		raw, err := base.Dial("tcp", proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("proxy: dial %s: %w", proxyAddr, err)
		}
		var conn net.Conn = raw
		if useTLS {
			conn = tls.Client(raw, &tls.Config{ServerName: cfg.Host})
			if err := conn.(*tls.Conn).Handshake(); err != nil {
				_ = raw.Close()
				return nil, fmt.Errorf("proxy: tls handshake to %s: %w", proxyAddr, err)
			}
		}
		req, err := http.NewRequest("CONNECT", "http://"+address, nil)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		req.Host = address
		if cfg.Username != "" || cfg.Password != "" {
			req.SetBasicAuth(cfg.Username, cfg.Password)
		}
		if err := req.Write(conn); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy: write CONNECT: %w", err)
		}
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, req)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy: read CONNECT response: %w", err)
		}
		if resp.StatusCode != 200 {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy: CONNECT to %s failed: %s", address, resp.Status)
		}
		return conn, nil
	}
}
