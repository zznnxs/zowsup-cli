package proxy

import "testing"

func TestParseTemplate(t *testing.T) {
	cases := []struct {
		in   string
		want Config
		err  bool
	}{
		{"", Config{}, false},
		{"DIRECT", Config{}, false},
		{"1.2.3.4:1080", Config{Scheme: "socks5", Host: "1.2.3.4", Port: 1080}, false},
		{"1.2.3.4:1080:u:p", Config{Scheme: "socks5", Host: "1.2.3.4", Port: 1080, Username: "u", Password: "p"}, false},
		{"http://1.2.3.4:8080:u:p", Config{Scheme: "http", Host: "1.2.3.4", Port: 8080, Username: "u", Password: "p"}, false},
		{"https://h:443:u:{session_id}", Config{Scheme: "https", Host: "h", Port: 443, Username: "u", Password: "{session_id}"}, false},
		{"socks5://h:1080", Config{Scheme: "socks5", Host: "h", Port: 1080}, false},
		{"ftp://h:21", Config{}, true},
		{"missing", Config{}, true},
		{"h:abc", Config{}, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseTemplate(c.in)
			if c.err {
				if err == nil {
					t.Fatalf("expected error for %q, got %+v", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestResolvedPlaceholders(t *testing.T) {
	c := Config{
		Scheme:   "http",
		Host:     "h",
		Port:     1,
		Username: "user-{location}",
		Password: "{session_id}-pw",
	}
	got := c.Resolved(Vars{SessionID: "S1", Location: "HK"})
	if got.Username != "user-HK" || got.Password != "S1-pw" {
		t.Fatalf("placeholders not expanded: %+v", got)
	}
}

func TestURL(t *testing.T) {
	c := Config{Scheme: "http", Host: "h", Port: 8080, Username: "u", Password: "p"}
	u := c.URL()
	if u == nil {
		t.Fatal("expected URL, got nil")
	}
	if u.Scheme != "http" || u.Host != "h:8080" || u.User.Username() != "u" {
		t.Fatalf("bad URL: %v", u)
	}

	if (Config{}).URL() != nil {
		t.Fatal("direct config should yield nil URL")
	}
}

func TestManagerCaching(t *testing.T) {
	m := NewManager()
	cfg := Config{Scheme: "http", Host: "h", Port: 8080}
	t1, err := m.Transport(cfg, Vars{})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := m.Transport(cfg, Vars{})
	if err != nil {
		t.Fatal(err)
	}
	if t1 != t2 {
		t.Fatal("manager should cache transport for identical resolved config")
	}
	// Different vars yielding different resolved string should produce a
	// different transport.
	cfg2 := Config{Scheme: "http", Host: "h", Port: 8080, Username: "u-{location}"}
	t3, err := m.Transport(cfg2, Vars{Location: "A"})
	if err != nil {
		t.Fatal(err)
	}
	t4, err := m.Transport(cfg2, Vars{Location: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if t3 == t4 {
		t.Fatal("different placeholder values should yield distinct transports")
	}
}
