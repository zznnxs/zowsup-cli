// Package proxy maps a per-account proxy configuration to a *http.Transport
// (and to a net.Conn dialer factory) that the rest of the system uses to
// reach WhatsApp servers.
//
// The Python tool accepts proxy strings of the form
//
//	host:port:username:password
//
// where username and password may embed {session_id} and {location}
// placeholders. The Go rewrite keeps the same wire format on the
// account-row level (`proxies.template`), but stores the parsed fields in
// dedicated columns; ParseTemplate handles both directions.
package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// Config describes a single proxy. The zero value (Scheme == "") means
// "direct connection, no proxy".
type Config struct {
	Scheme   string // "socks5", "http", "https", or ""
	Host     string
	Port     int
	Username string // may contain {session_id} / {location}
	Password string // may contain {session_id} / {location}
}

// IsDirect reports whether c represents a no-proxy direct connection.
func (c Config) IsDirect() bool { return c.Scheme == "" }

// String returns the canonical "host:port:user:pass" form, for storage.
func (c Config) String() string {
	if c.IsDirect() {
		return ""
	}
	return fmt.Sprintf("%s:%d:%s:%s", c.Host, c.Port, c.Username, c.Password)
}

// ParseTemplate parses a "host:port[:user[:pass]]" template string. Scheme
// defaults to socks5 unless explicitly specified via a "<scheme>://" prefix.
func ParseTemplate(s string) (Config, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "DIRECT") {
		return Config{}, nil
	}
	scheme := "socks5"
	if i := strings.Index(s, "://"); i != -1 {
		scheme = strings.ToLower(s[:i])
		s = s[i+3:]
	}
	switch scheme {
	case "socks5", "http", "https":
	default:
		return Config{}, fmt.Errorf("proxy: unsupported scheme %q", scheme)
	}
	parts := strings.SplitN(s, ":", 4)
	if len(parts) < 2 {
		return Config{}, fmt.Errorf("proxy: template must contain at least host:port, got %q", s)
	}
	port, err := parsePort(parts[1])
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Scheme: scheme, Host: parts[0], Port: port}
	if len(parts) >= 3 {
		cfg.Username = parts[2]
	}
	if len(parts) >= 4 {
		cfg.Password = parts[3]
	}
	return cfg, nil
}

func parsePort(s string) (int, error) {
	if s == "" {
		return 0, errors.New("proxy: empty port")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("proxy: non-numeric port %q", s)
		}
		n = n*10 + int(c-'0')
		if n > 65535 {
			return 0, fmt.Errorf("proxy: port %q out of range", s)
		}
	}
	if n == 0 {
		return 0, errors.New("proxy: port must be non-zero")
	}
	return n, nil
}

// Vars is the substitution context for {session_id} and {location}
// placeholders. Either field may be empty.
type Vars struct {
	SessionID string
	Location  string
}

// Resolved returns a copy of c with username/password placeholders expanded
// from v.
func (c Config) Resolved(v Vars) Config {
	out := c
	out.Username = expandPlaceholders(c.Username, v)
	out.Password = expandPlaceholders(c.Password, v)
	return out
}

func expandPlaceholders(s string, v Vars) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer(
		"{session_id}", v.SessionID,
		"{location}", v.Location,
	)
	return r.Replace(s)
}

// URL builds a *url.URL suitable for http.Transport.Proxy. Returns nil for
// direct (no-proxy) configs.
func (c Config) URL() *url.URL {
	if c.IsDirect() {
		return nil
	}
	u := &url.URL{
		Scheme: c.Scheme,
		Host:   net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port)),
	}
	if c.Username != "" || c.Password != "" {
		u.User = url.UserPassword(c.Username, c.Password)
	}
	return u
}

// Dialer is the abstract dial function used by everything that opens a
// connection. For HTTP/HTTPS proxies we go through net/http; for SOCKS5 we
// build a stream-level proxy.Dialer.
type Dialer func(network, address string) (net.Conn, error)

// Manager caches resolved transports and dialers keyed by config string.
// Building a transport is cheap, but keeping a single instance per
// configuration lets us share connection pools.
type Manager struct {
	mu         sync.Mutex
	transports map[string]*http.Transport
}

// NewManager returns a fresh empty Manager.
func NewManager() *Manager {
	return &Manager{transports: map[string]*http.Transport{}}
}

// HTTPClient returns a *http.Client whose Transport routes through cfg
// (after placeholder resolution).
func (m *Manager) HTTPClient(cfg Config, v Vars) (*http.Client, error) {
	t, err := m.Transport(cfg, v)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: t, Timeout: 30 * time.Second}, nil
}

// Transport returns a cached *http.Transport for the (cfg,v) pair.
func (m *Manager) Transport(cfg Config, v Vars) (*http.Transport, error) {
	resolved := cfg.Resolved(v)
	key := resolved.String()
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.transports[key]; ok {
		return t, nil
	}
	t, err := buildTransport(resolved)
	if err != nil {
		return nil, err
	}
	m.transports[key] = t
	return t, nil
}

// Dialer returns a stream dialer for cfg. Useful for raw TCP / TLS / WS
// connections that don't go through net/http.
func (m *Manager) Dialer(cfg Config, v Vars) (Dialer, error) {
	resolved := cfg.Resolved(v)
	if resolved.IsDirect() {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		return d.Dial, nil
	}
	switch resolved.Scheme {
	case "socks5":
		var auth *proxy.Auth
		if resolved.Username != "" || resolved.Password != "" {
			auth = &proxy.Auth{User: resolved.Username, Password: resolved.Password}
		}
		base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		d, err := proxy.SOCKS5("tcp", net.JoinHostPort(resolved.Host, fmt.Sprintf("%d", resolved.Port)), auth, base)
		if err != nil {
			return nil, fmt.Errorf("proxy: socks5 dialer: %w", err)
		}
		return d.Dial, nil
	case "http", "https":
		// CONNECT proxies are typically used via net/http.Transport; we
		// expose a minimal CONNECT dialer here so callers that need a raw
		// TCP stream can still tunnel.
		return newConnectDialer(resolved), nil
	}
	return nil, fmt.Errorf("proxy: unsupported scheme %q", resolved.Scheme)
}

// Reset drops cached transports — call after a proxy config edit so the
// next request rebuilds from scratch.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.transports {
		t.CloseIdleConnections()
	}
	m.transports = map[string]*http.Transport{}
}

func buildTransport(cfg Config) (*http.Transport, error) {
	t := &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if cfg.IsDirect() {
		return t, nil
	}
	switch cfg.Scheme {
	case "http", "https":
		u := cfg.URL()
		t.Proxy = http.ProxyURL(u)
	case "socks5":
		var auth *proxy.Auth
		if cfg.Username != "" || cfg.Password != "" {
			auth = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
		}
		base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		d, err := proxy.SOCKS5("tcp", net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)), auth, base)
		if err != nil {
			return nil, fmt.Errorf("proxy: socks5 transport: %w", err)
		}
		t.Dial = d.Dial
	}
	return t, nil
}
