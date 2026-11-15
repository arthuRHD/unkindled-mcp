package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoSendsBearerAuthAndDecodesResponse(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ws_1","status":"running","n":12}`))
	}))
	defer upstream.Close()

	client := New(upstream.URL, "test-token", 2*time.Second)
	response, err := client.Do(context.Background(), http.MethodGet, "/v1/workstations", nil)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/workstations" {
		t.Fatalf("got %s %s, want GET /v1/workstations", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	data, ok := client.Decode(response.Body).(map[string]any)
	if !ok {
		t.Fatalf("decoded body has type %T, want object", client.Decode(response.Body))
	}
	if data["id"] != "ws_1" || data["status"] != "running" {
		t.Fatalf("unexpected decoded body: %#v", data)
	}
}

func TestAPIErrorIncludesStatusAndRedactsToken(t *testing.T) {
	t.Parallel()

	const token = "super-secret-token"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token super-secret-token"}}`))
	}))
	defer upstream.Close()

	client := New(upstream.URL, token, 2*time.Second)
	response, err := client.Do(context.Background(), http.MethodGet, "/v1/workstations", nil)
	if err == nil {
		t.Fatal("expected an API error")
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("response status = %d, want 401", response.StatusCode)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("APIError status = %d, want 401", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "HTTP 401") {
		t.Fatalf("error %q does not include HTTP status", apiErr.Error())
	}
	if !strings.Contains(apiErr.Error(), "invalid token") {
		t.Fatalf("error %q does not include API message", apiErr.Error())
	}
	if strings.Contains(apiErr.Error(), token) {
		t.Fatalf("error leaks token: %q", apiErr.Error())
	}
	if !strings.Contains(apiErr.Error(), "[REDACTED]") {
		t.Fatalf("error does not show redaction: %q", apiErr.Error())
	}
}

func TestClientTimeout(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	client := New(upstream.URL, "token", 20*time.Millisecond)
	start := time.Now()
	_, err := client.Do(context.Background(), http.MethodGet, "/v1/workstations", nil)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
	if !strings.Contains(err.Error(), "request to Unkindled API failed") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func TestDecodeSanitizesSecrets(t *testing.T) {
	t.Parallel()

	const token = "super-secret-token"
	client := New("https://api.example.test", token, time.Second)
	body := []byte(`{
		"private_key":"-----BEGIN OPENSSH PRIVATE KEY-----",
		"next_token":"keep-me",
		"public_key":"ssh-ed25519 AAAA",
		"nested":{"password":"hunter2","note":"token super-secret-token"},
		"items":[{"provider_secret":"provider-secret","name":"one"}]
	}`)

	value, ok := client.Decode(body).(map[string]any)
	if !ok {
		t.Fatal("decoded body is not an object")
	}
	if value["private_key"] != "[REDACTED]" {
		t.Fatalf("private_key was not redacted: %#v", value["private_key"])
	}
	if value["next_token"] != "keep-me" {
		t.Fatalf("pagination token should be preserved: %#v", value["next_token"])
	}
	if value["public_key"] != "ssh-ed25519 AAAA" {
		t.Fatalf("public key should be preserved: %#v", value["public_key"])
	}

	nested, _ := value["nested"].(map[string]any)
	if nested["password"] != "[REDACTED]" {
		t.Fatalf("password was not redacted: %#v", nested["password"])
	}
	if strings.Contains(nested["note"].(string), token) {
		t.Fatalf("embedded token was not redacted: %#v", nested["note"])
	}

	items, _ := value["items"].([]any)
	first, _ := items[0].(map[string]any)
	if first["provider_secret"] != "[REDACTED]" || first["name"] != "one" {
		t.Fatalf("nested array was not sanitized: %#v", first)
	}
}

func TestErrorMessageFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "plain text", body: "gateway exploded", want: "gateway exploded"},
		{name: "message field", body: `{"message":"nope"}`, want: "nope"},
		{name: "list error", body: `{"errors":[{"detail":"first problem"}]}`, want: "first problem"},
		{name: "empty", body: "", want: "no error details were returned"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := New("https://api.example.test", "token", time.Second)
			got := client.errorMessage([]byte(test.body))
			if got != test.want {
				t.Fatalf("errorMessage(%q) = %q, want %q", test.body, got, test.want)
			}
		})
	}
}
