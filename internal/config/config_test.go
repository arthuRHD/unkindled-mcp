package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("UNKINDLED_API_URL", "")
	t.Setenv("UNKINDLED_API_TOKEN", "")
	t.Setenv("UNKINDLED_HTTP_TIMEOUT", "")

	cfg, warnings := Load()
	if cfg.APIURL != DefaultAPIURL {
		t.Fatalf("APIURL = %q, want %q", cfg.APIURL, DefaultAPIURL)
	}
	if cfg.Token != "" {
		t.Fatalf("Token = %q, want empty", cfg.Token)
	}
	if cfg.Timeout != DefaultHTTPTimeout {
		t.Fatalf("Timeout = %s, want %s", cfg.Timeout, DefaultHTTPTimeout)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestLoadCustomValues(t *testing.T) {
	t.Setenv("UNKINDLED_API_URL", "https://api.example.test")
	t.Setenv("UNKINDLED_API_TOKEN", "  test-token  ")
	t.Setenv("UNKINDLED_HTTP_TIMEOUT", "5s")

	cfg, warnings := Load()
	if cfg.APIURL != "https://api.example.test" {
		t.Fatalf("APIURL = %q", cfg.APIURL)
	}
	if cfg.Token != "test-token" {
		t.Fatalf("Token = %q, want trimmed token", cfg.Token)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s, want 5s", cfg.Timeout)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestLoadBareSecondsTimeout(t *testing.T) {
	t.Setenv("UNKINDLED_API_URL", "")
	t.Setenv("UNKINDLED_API_TOKEN", "token")
	t.Setenv("UNKINDLED_HTTP_TIMEOUT", "45")

	cfg, warnings := Load()
	if cfg.Timeout != 45*time.Second {
		t.Fatalf("Timeout = %s, want 45s", cfg.Timeout)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestLoadInvalidTimeoutFallsBackWithWarning(t *testing.T) {
	t.Setenv("UNKINDLED_API_URL", "")
	t.Setenv("UNKINDLED_API_TOKEN", "token")
	t.Setenv("UNKINDLED_HTTP_TIMEOUT", "not-a-duration")

	cfg, warnings := Load()
	if cfg.Timeout != DefaultHTTPTimeout {
		t.Fatalf("Timeout = %s, want default %s", cfg.Timeout, DefaultHTTPTimeout)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "UNKINDLED_HTTP_TIMEOUT") {
		t.Fatalf("warnings = %v, want one UNKINDLED_HTTP_TIMEOUT warning", warnings)
	}
}

func TestLoadNonPositiveTimeoutFallsBackWithWarning(t *testing.T) {
	t.Setenv("UNKINDLED_API_URL", "")
	t.Setenv("UNKINDLED_API_TOKEN", "token")
	t.Setenv("UNKINDLED_HTTP_TIMEOUT", "0s")

	cfg, warnings := Load()
	if cfg.Timeout != DefaultHTTPTimeout {
		t.Fatalf("Timeout = %s, want default %s", cfg.Timeout, DefaultHTTPTimeout)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "must be positive") {
		t.Fatalf("warnings = %v, want one positive-timeout warning", warnings)
	}
}
