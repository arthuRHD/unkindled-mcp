// Package config loads the unkindled MCP server configuration from the
// environment. Missing credentials are not fatal at startup: the server must
// still initialize so MCP clients can display a useful configuration error.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAPIURL is the public Unkindled API endpoint.
	DefaultAPIURL = "https://api.unkindled.dev"

	// DefaultHTTPTimeout is used when UNKINDLED_HTTP_TIMEOUT is unset.
	DefaultHTTPTimeout = 30 * time.Second
)

// Config is the runtime configuration for the server and its API client.
type Config struct {
	APIURL  string
	Token   string
	Timeout time.Duration
}

// Load reads configuration from the environment. It always returns a usable
// Config; non-fatal problems are returned as human-readable warnings for the
// caller to print on stderr.
func Load() (Config, []string) {
	cfg := Config{
		APIURL:  DefaultAPIURL,
		Token:   strings.TrimSpace(os.Getenv("UNKINDLED_API_TOKEN")),
		Timeout: DefaultHTTPTimeout,
	}

	var warnings []string

	if raw := strings.TrimSpace(os.Getenv("UNKINDLED_API_URL")); raw != "" {
		cfg.APIURL = raw
	}

	if raw := strings.TrimSpace(os.Getenv("UNKINDLED_HTTP_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			// Accept a bare integer as a number of seconds for convenience.
			if seconds, convertErr := strconv.Atoi(raw); convertErr == nil {
				timeout = time.Duration(seconds) * time.Second
				err = nil
			}
		}
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf(
				"invalid UNKINDLED_HTTP_TIMEOUT %q; using %s", raw, DefaultHTTPTimeout))
		case timeout <= 0:
			warnings = append(warnings, fmt.Sprintf(
				"UNKINDLED_HTTP_TIMEOUT must be positive; using %s", DefaultHTTPTimeout))
		default:
			cfg.Timeout = timeout
		}
	}

	return cfg, warnings
}
