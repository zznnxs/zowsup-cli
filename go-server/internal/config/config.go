// Package config loads the small server-side config used by the Go
// rewrite. Values can be passed via env vars or a single flag.
package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
)

// Config is the resolved runtime configuration.
type Config struct {
	// HTTPAddr is the address chi listens on. Default ":8080".
	HTTPAddr string
	// DBPath is the sqlite file path. Default "./data/zowsup.db".
	DBPath string
	// FrontendDir, if non-empty, is served as static files at "/" so the
	// single binary can host both API and built frontend. Empty in dev.
	FrontendDir string
	// EventBusBuffer is the per-subscriber buffer size for the event bus.
	EventBusBuffer int
}

// Default returns a Config populated from environment variables and sane
// defaults, without parsing any flags. Suitable for tests.
func Default() Config {
	return Config{
		HTTPAddr:       getenv("ZOWSUP_HTTP_ADDR", ":8080"),
		DBPath:         getenv("ZOWSUP_DB_PATH", filepath.Join("data", "zowsup.db")),
		FrontendDir:    os.Getenv("ZOWSUP_FRONTEND_DIR"),
		EventBusBuffer: 64,
	}
}

// FromFlags parses os.Args. Returns an error rather than os.Exit so callers
// can decide what to do.
func FromFlags(args []string) (Config, error) {
	c := Default()
	fs := flag.NewFlagSet("zowsup-go", flag.ContinueOnError)
	fs.StringVar(&c.HTTPAddr, "http", c.HTTPAddr, "HTTP listen address")
	fs.StringVar(&c.DBPath, "db", c.DBPath, "sqlite database path")
	fs.StringVar(&c.FrontendDir, "frontend", c.FrontendDir, "directory of pre-built frontend assets (optional)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if c.HTTPAddr == "" {
		return Config{}, errors.New("config: --http must not be empty")
	}
	if c.DBPath == "" {
		return Config{}, errors.New("config: --db must not be empty")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
