// Package api implements a small, dependency-free client for the Unkindled
// JSON API. It handles bearer authentication, request timeouts, response size
// limits, readable API errors, and redaction of credentials.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// maxResponseBytes bounds how much of an API response is read into memory.
const maxResponseBytes = 8 << 20

// Client is a Unkindled API client. It is safe for concurrent use.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Response is a successful (2xx) API response.
type Response struct {
	StatusCode int
	Body       []byte
}

// APIError describes a non-2xx response from the Unkindled API. Its message is
// already redacted and safe to show to an MCP client.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Unkindled API returned HTTP %d: %s", e.StatusCode, e.Message)
}

// New returns a client for baseURL. An empty or non-positive timeout falls back
// to 30 seconds.
func New(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Do sends a JSON request to path. A non-2xx response returns both the response
// and an *APIError so callers can report the HTTP status code. Transport errors
// and API error messages are redacted.
func (c *Client) Do(ctx context.Context, method, path string, body any) (Response, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return Response{}, fmt.Errorf("build request: %s", c.Redact(err.Error()))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "unkindled-mcp")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("request to Unkindled API failed: %s", c.Redact(err.Error()))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, fmt.Errorf("read Unkindled API response: %s", c.Redact(err.Error()))
	}
	if len(data) > maxResponseBytes {
		return Response{}, fmt.Errorf("Unkindled API response exceeded %d bytes", maxResponseBytes)
	}

	result := Response{StatusCode: resp.StatusCode, Body: data}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return result, &APIError{
			StatusCode: resp.StatusCode,
			Message:    c.errorMessage(data),
		}
	}
	return result, nil
}

// Decode converts a JSON response body into a generic value, preserving number
// formatting where possible. Secret-looking fields and any embedded API token
// are redacted. Non-JSON bodies are returned as a redacted string.
func (c *Client) Decode(body []byte) any {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return c.Redact(string(trimmed))
	}
	return SanitizeValue(value, c.token)
}

// Redact removes the bearer token and common bearer-token syntax from s.
func (c *Client) Redact(s string) string {
	return redactString(s, c.token)
}

func (c *Client) errorMessage(body []byte) string {
	message := extractAPIMessage(body)
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = "no error details were returned"
	}
	return c.Redact(truncate(message, 600))
}

// extractAPIMessage finds a human-readable message in the common JSON error
// shapes returned by APIs.
func extractAPIMessage(body []byte) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	return findErrorMessage(value)
}

func findErrorMessage(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"error", "errors", "message", "detail", "error_description"} {
			entry, ok := typed[key]
			if !ok {
				continue
			}
			switch inner := entry.(type) {
			case string:
				if strings.TrimSpace(inner) != "" {
					return strings.TrimSpace(inner)
				}
			case map[string]any:
				if message := findErrorMessage(inner); message != "" {
					return message
				}
			case []any:
				for _, item := range inner {
					if message := findErrorMessage(item); message != "" {
						return message
					}
				}
			}
		}
	case []any:
		for _, item := range typed {
			if message := findErrorMessage(item); message != "" {
				return message
			}
		}
	}
	return ""
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)

func redactString(s, token string) string {
	if s == "" {
		return s
	}
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	if token != "" {
		s = strings.ReplaceAll(s, token, "[REDACTED]")
		if escaped := url.QueryEscape(token); escaped != token {
			s = strings.ReplaceAll(s, escaped, "[REDACTED]")
		}
	}
	return s
}

// SanitizeValue recursively redacts secret-looking keys and token occurrences
// in strings. It is exported for use by tests and defensive post-processing.
func SanitizeValue(value any, token string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, entry := range typed {
			if isSecretKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = SanitizeValue(entry, token)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, entry := range typed {
			out[i] = SanitizeValue(entry, token)
		}
		return out
	case string:
		return redactString(typed, token)
	default:
		return value
	}
}

func isSecretKey(key string) bool {
	normalized := strings.ToLower(key)
	normalized = strings.NewReplacer("_", "", "-", "", " ", "").Replace(normalized)

	switch normalized {
	case "privatekey", "sshprivatekey", "privatekeypem", "password",
		"passphrase", "secret", "clientsecret", "providersecret", "apikey",
		"apitoken", "authtoken", "bearertoken", "sessiontoken", "idtoken",
		"token", "accesstoken", "refreshtoken", "authorization", "credentials",
		"secretkey", "connectionsecret":
		return true
	}

	return strings.HasSuffix(normalized, "password") ||
		strings.HasSuffix(normalized, "secret") ||
		strings.HasSuffix(normalized, "apikey") ||
		strings.HasSuffix(normalized, "privatekey")
}
