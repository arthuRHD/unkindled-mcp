package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arthuRHD/unkindled-mcp/internal/api"
)

type rpcEnvelope struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  map[string]any `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newTestServer(t *testing.T, token string, handler http.HandlerFunc) *Server {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	client := api.New(upstream.URL, token, 2*time.Second)
	return NewServer(client, token != "")
}

func runRaw(t *testing.T, server *Server, lines ...string) string {
	t.Helper()
	var output bytes.Buffer
	input := strings.Join(lines, "\n")
	if input != "" {
		input += "\n"
	}
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
	return output.String()
}

func runRPC(t *testing.T, server *Server, lines ...string) []rpcEnvelope {
	t.Helper()
	raw := strings.TrimSpace(runRaw(t, server, lines...))
	if raw == "" {
		return nil
	}

	rawLines := strings.Split(raw, "\n")
	envelopes := make([]rpcEnvelope, 0, len(rawLines))
	for _, line := range rawLines {
		var envelope rpcEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("response is not valid JSON: %q: %v", line, err)
		}
		if envelope.JSONRPC != "2.0" {
			t.Fatalf("response jsonrpc = %q, want 2.0", envelope.JSONRPC)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func toolCallLine(t *testing.T, id int, name string, arguments map[string]any) string {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		t.Fatalf("marshal tool call: %v", err)
	}
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`, id, params)
}

func contentTexts(t *testing.T, result map[string]any) []string {
	t.Helper()
	rawContent, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("result content has type %T, want array", result["content"])
	}
	texts := make([]string, 0, len(rawContent))
	for _, item := range rawContent {
		block, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("content block has type %T, want object", item)
		}
		if block["type"] != "text" {
			t.Fatalf("content type = %v, want text", block["type"])
		}
		text, ok := block["text"].(string)
		if !ok {
			t.Fatalf("content text has type %T, want string", block["text"])
		}
		texts = append(texts, text)
	}
	return texts
}

func resultIsError(result map[string]any) bool {
	isError, _ := result["isError"].(bool)
	return isError
}

func TestInitializeToolsListAndPing(t *testing.T) {
	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API request: %s %s", r.Method, r.URL.String())
		w.WriteHeader(http.StatusInternalServerError)
	})

	responses := runRPC(t, server,
		`{"jsonrpc":"2.0","id":"init-1","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping","params":{}}`,
	)
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3 (notifications must not be answered)", len(responses))
	}

	initialize := responses[0]
	if initialize.ID != "init-1" {
		t.Fatalf("initialize id = %#v, want init-1", initialize.ID)
	}
	if initialize.Error != nil {
		t.Fatalf("initialize error: %+v", initialize.Error)
	}
	if initialize.Result["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion = %v, want %s", initialize.Result["protocolVersion"], protocolVersion)
	}
	capabilities, _ := initialize.Result["capabilities"].(map[string]any)
	toolsCapability, ok := capabilities["tools"].(map[string]any)
	if !ok || len(toolsCapability) != 0 {
		t.Fatalf("capabilities.tools = %#v, want empty object", capabilities["tools"])
	}
	serverInfo, _ := initialize.Result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "unkindled-mcp" || serverInfo["version"] == "" {
		t.Fatalf("unexpected serverInfo: %#v", serverInfo)
	}
	if instructions, _ := initialize.Result["instructions"].(string); instructions == "" {
		t.Fatal("initialize did not include instructions")
	}

	list := responses[1]
	if list.Error != nil {
		t.Fatalf("tools/list error: %+v", list.Error)
	}
	rawTools, ok := list.Result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result has no tools array: %#v", list.Result)
	}

	wantNames := []string{
		"list_workstations", "get_workstation", "get_connection",
		"start_workstation", "stop_workstation", "restart_workstation",
		"create_checkpoint", "list_checkpoints", "restore_checkpoint",
		"delete_workstation", "list_events",
	}
	if len(rawTools) != len(wantNames) {
		t.Fatalf("got %d tools, want %d", len(rawTools), len(wantNames))
	}

	toolsByName := make(map[string]map[string]any, len(rawTools))
	for i, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("tool has type %T, want object", rawTool)
		}
		name, _ := tool["name"].(string)
		if name != wantNames[i] {
			t.Fatalf("tool %d = %q, want %q", i, name, wantNames[i])
		}
		if description, _ := tool["description"].(string); description == "" {
			t.Fatalf("tool %q has no description", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q has no inputSchema", name)
		}
		if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("tool %q must set additionalProperties:false", name)
		}
		if _, ok := schema["required"].([]any); !ok {
			t.Fatalf("tool %q schema required must be an array", name)
		}
		toolsByName[name] = tool
	}

	for _, name := range []string{"restore_checkpoint", "delete_workstation"} {
		tool := toolsByName[name]
		schema := tool["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		confirm, ok := properties["confirm"].(map[string]any)
		if !ok || confirm["type"] != "boolean" {
			t.Fatalf("%s must have a boolean confirm property", name)
		}
		required := fmt.Sprint(schema["required"])
		if !strings.Contains(required, "confirm") {
			t.Fatalf("%s must require confirm; required = %v", name, schema["required"])
		}
	}

	ping := responses[2]
	if ping.Error != nil || ping.Result == nil || len(ping.Result) != 0 {
		t.Fatalf("unexpected ping response: %+v", ping)
	}
}

func TestToolRouting(t *testing.T) {
	type record struct {
		method string
		uri    string
		body   string
	}
	var (
		mu      sync.Mutex
		records []record
	)

	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		mu.Lock()
		records = append(records, record{method: r.Method, uri: r.URL.RequestURI(), body: string(body)})
		mu.Unlock()

		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	tests := []struct {
		name   string
		args   map[string]any
		method string
		uri    string
	}{
		{"list_workstations", map[string]any{"include_deleted": true}, http.MethodGet, "/v1/workstations?include_deleted=true"},
		{"get_workstation", map[string]any{"id": "ws_1"}, http.MethodGet, "/v1/workstations/ws_1"},
		{"get_connection", map[string]any{"id": "ws_1"}, http.MethodGet, "/v1/workstations/ws_1/connection"},
		{"start_workstation", map[string]any{"id": "ws_1"}, http.MethodPost, "/v1/workstations/ws_1/start"},
		{"stop_workstation", map[string]any{"id": "ws_1"}, http.MethodPost, "/v1/workstations/ws_1/stop"},
		{"restart_workstation", map[string]any{"id": "ws_1"}, http.MethodPost, "/v1/workstations/ws_1/restart"},
		{"create_checkpoint", map[string]any{"id": "ws_1", "label": "pre-upgrade", "reason": "before upgrade"}, http.MethodPost, "/v1/workstations/ws_1/checkpoints"},
		{"list_checkpoints", map[string]any{"id": "ws_1"}, http.MethodGet, "/v1/workstations/ws_1/checkpoints"},
		{"restore_checkpoint", map[string]any{"id": "ws_1", "checkpoint_id": "cp_1", "confirm": true}, http.MethodPost, "/v1/workstations/ws_1/checkpoints/cp_1/restore"},
		{"delete_workstation", map[string]any{"id": "ws_1", "confirm": true}, http.MethodDelete, "/v1/workstations/ws_1"},
		{"list_events", map[string]any{"id": "ws_1"}, http.MethodGet, "/v1/workstations/ws_1/events"},
	}

	for i, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			responses := runRPC(t, server, toolCallLine(t, i+1, test.name, test.args))
			if len(responses) != 1 {
				t.Fatalf("got %d responses, want 1", len(responses))
			}
			if responses[0].Error != nil {
				t.Fatalf("JSON-RPC error: %+v", responses[0].Error)
			}
			if resultIsError(responses[0].Result) {
				t.Fatalf("tool error: %v", contentTexts(t, responses[0].Result))
			}
			texts := contentTexts(t, responses[0].Result)
			if len(texts) != 2 {
				t.Fatalf("got %d content blocks, want summary and JSON", len(texts))
			}
			if !strings.Contains(texts[0], "HTTP") {
				t.Fatalf("summary does not include HTTP status: %q", texts[0])
			}
		})
	}

	mu.Lock()
	defer mu.Unlock()
	if len(records) != len(tests) {
		t.Fatalf("got %d API calls, want %d", len(records), len(tests))
	}
	for i, test := range tests {
		got := records[i]
		if got.method != test.method || got.uri != test.uri {
			t.Fatalf("%s: got %s %s, want %s %s", test.name, got.method, got.uri, test.method, test.uri)
		}
	}

	var checkpointBody map[string]any
	if err := json.Unmarshal([]byte(records[6].body), &checkpointBody); err != nil {
		t.Fatalf("checkpoint body is not JSON: %v", err)
	}
	if checkpointBody["label"] != "pre-upgrade" || checkpointBody["reason"] != "before upgrade" {
		t.Fatalf("unexpected checkpoint body: %#v", checkpointBody)
	}
}

func TestToolErrorMapping(t *testing.T) {
	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workstations/ws_missing" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"workstation ws_missing not found"}}`))
	})

	responses := runRPC(t, server, toolCallLine(t, 1, "get_workstation", map[string]any{"id": "ws_missing"}))
	if len(responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(responses))
	}
	response := responses[0]
	if response.Error != nil {
		t.Fatalf("API errors must be tool results, got JSON-RPC error: %+v", response.Error)
	}
	if !resultIsError(response.Result) {
		t.Fatal("result must set isError:true")
	}
	texts := contentTexts(t, response.Result)
	if len(texts) != 1 {
		t.Fatalf("got %d error text blocks, want 1", len(texts))
	}
	for _, want := range []string{"HTTP 404", "workstation ws_missing not found"} {
		if !strings.Contains(texts[0], want) {
			t.Fatalf("error text %q does not include %q", texts[0], want)
		}
	}
}

func TestPendingStatusIsReported(t *testing.T) {
	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workstations/ws_1/start" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"ws_1","status":"starting","operation_id":"op_1"}`))
	})

	responses := runRPC(t, server, toolCallLine(t, 1, "start_workstation", map[string]any{"id": "ws_1"}))
	response := responses[0]
	if response.Error != nil || resultIsError(response.Result) {
		t.Fatalf("unexpected error: %+v / %#v", response.Error, response.Result)
	}
	texts := contentTexts(t, response.Result)
	if !strings.Contains(texts[0], "HTTP 202") || !strings.Contains(strings.ToLower(texts[0]), "pending") {
		t.Fatalf("summary does not report pending HTTP 202: %q", texts[0])
	}
	if !strings.Contains(texts[1], `"status":"starting"`) {
		t.Fatalf("JSON block does not include API status: %q", texts[1])
	}
}

func TestDestructiveToolsRequireConfirm(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var paths []string

	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"delete_workstation", map[string]any{"id": "ws_1", "confirm": false}, "Nothing was done"},
		{"restore_checkpoint", map[string]any{"id": "ws_1", "checkpoint_id": "cp_1", "confirm": false}, "Nothing was done"},
	}

	for i, test := range cases {
		responses := runRPC(t, server, toolCallLine(t, i+1, test.tool, test.args))
		response := responses[0]
		if response.Error != nil || resultIsError(response.Result) {
			t.Fatalf("%s returned an error for a safety stop: %+v / %#v", test.tool, response.Error, response.Result)
		}
		texts := contentTexts(t, response.Result)
		if !strings.Contains(texts[0], test.want) || !strings.Contains(texts[0], "confirm") {
			t.Fatalf("%s summary = %q, want explanation that nothing was done", test.tool, texts[0])
		}
		if len(texts) != 2 || !strings.Contains(texts[1], `"performed":false`) {
			t.Fatalf("%s JSON = %v, want performed:false", test.tool, texts)
		}
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("API was called %d times when confirm was false, want 0", got)
	}

	confirmed := []struct {
		tool string
		args map[string]any
	}{
		{"delete_workstation", map[string]any{"id": "ws_1", "confirm": true}},
		{"restore_checkpoint", map[string]any{"id": "ws_1", "checkpoint_id": "cp_1", "confirm": true}},
	}
	for i, test := range confirmed {
		responses := runRPC(t, server, toolCallLine(t, i+10, test.tool, test.args))
		if resultIsError(responses[0].Result) {
			t.Fatalf("%s with confirm=true failed: %v", test.tool, contentTexts(t, responses[0].Result))
		}
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("API calls = %d, want 2 after confirmed operations", got)
	}
	mu.Lock()
	defer mu.Unlock()
	wantPaths := []string{"DELETE /v1/workstations/ws_1", "POST /v1/workstations/ws_1/checkpoints/cp_1/restore"}
	for i, want := range wantPaths {
		if paths[i] != want {
			t.Fatalf("path %d = %q, want %q", i, paths[i], want)
		}
	}
}

func TestMissingTokenStillInitializes(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	responses := runRPC(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		toolCallLine(t, 2, "list_workstations", map[string]any{}),
	)
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want 2", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("initialize failed without a token: %+v", responses[0].Error)
	}
	if !resultIsError(responses[1].Result) {
		t.Fatal("tool call without a token must return isError:true")
	}
	texts := contentTexts(t, responses[1].Result)
	if !strings.Contains(texts[0], "UNKINDLED_API_TOKEN") || !strings.Contains(texts[0], "no request was sent") {
		t.Fatalf("missing-token message is unclear: %q", texts[0])
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("API calls = %d, want 0 without a token", got)
	}
}

func TestTokenIsNeverPrinted(t *testing.T) {
	const token = "sk-live-super-secret-token"
	server := newTestServer(t, token, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"invalid token sk-live-super-secret-token"}`))
	})

	raw := runRaw(t, server, toolCallLine(t, 1, "list_workstations", map[string]any{}))
	if strings.Contains(raw, token) {
		t.Fatalf("protocol output leaked the token: %q", raw)
	}

	responses := runRPC(t, server, toolCallLine(t, 2, "list_workstations", map[string]any{}))
	texts := contentTexts(t, responses[0].Result)
	if !strings.Contains(texts[0], "[REDACTED]") || !strings.Contains(texts[0], "HTTP 500") {
		t.Fatalf("error does not show redacted token and status: %q", texts[0])
	}
}

func TestGetConnectionRedactsPrivateKeysAndSecrets(t *testing.T) {
	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workstations/ws_1/connection" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"host":"ws1.example.test",
			"username":"dev",
			"public_key":"ssh-ed25519 AAAA",
			"private_key":"-----BEGIN OPENSSH PRIVATE KEY-----",
			"provider":{"api_token":"provider-secret","name":"example-cloud"}
		}`))
	})

	responses := runRPC(t, server, toolCallLine(t, 1, "get_connection", map[string]any{"id": "ws_1"}))
	if resultIsError(responses[0].Result) {
		t.Fatalf("unexpected tool error: %v", contentTexts(t, responses[0].Result))
	}
	texts := contentTexts(t, responses[0].Result)
	joined := strings.Join(texts, "\n")
	for _, forbidden := range []string{"BEGIN OPENSSH PRIVATE KEY", "provider-secret"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("connection output leaks %q: %s", forbidden, joined)
		}
	}
	for _, required := range []string{"ws1.example.test", "ssh-ed25519 AAAA", "[REDACTED]"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("connection output is missing %q: %s", required, joined)
		}
	}
}

func TestMalformedAndUnknownRequests(t *testing.T) {
	server := newTestServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.String())
	})

	responses := runRPC(t, server,
		`{`,
		`{"jsonrpc":"2.0","id":2,"method":"unknown/method"}`,
		`{"jsonrpc":"2.0","id":3}`,
		`[]`,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`,
		`"hello"`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
		`{"jsonrpc":"2.0","method":"unknown/notification"}`,
		`{"jsonrpc":"2.0","id":9,"method":"notifications/initialized"}`,
	)
	if len(responses) != 7 {
		t.Fatalf("got %d responses, want 7; notifications and malformed lines must not cause crashes", len(responses))
	}

	wantCodes := []int{-32700, -32601, -32600, -32600, -32600, -32600, 0}
	for i, want := range wantCodes {
		got := 0
		if responses[i].Error != nil {
			got = responses[i].Error.Code
		}
		if got != want {
			t.Fatalf("response %d error code = %d, want %d (response: %+v)", i, got, want, responses[i])
		}
	}
	if responses[6].Result == nil || len(responses[6].Result) != 0 {
		t.Fatalf("valid ping after errors did not succeed: %+v", responses[6])
	}
	if responses[0].ID != nil {
		t.Fatalf("parse errors should use a null id: %#v", responses[0].ID)
	}
	if responses[2].ID != float64(3) {
		t.Fatalf("invalid request id = %#v, want 3", responses[2].ID)
	}
}

func TestInvalidToolArgumentsAndUnknownTool(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	lines := []string{
		toolCallLine(t, 1, "get_workstation", map[string]any{"id": "ws_1", "bogus": 1}),
		toolCallLine(t, 2, "delete_workstation", map[string]any{"id": "ws_1"}),
		toolCallLine(t, 3, "get_workstation", map[string]any{"id": ""}),
		toolCallLine(t, 4, "no_such_tool", map[string]any{}),
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_workstations","arguments":[1]}}`,
	}
	responses := runRPC(t, server, lines...)
	if len(responses) != len(lines) {
		t.Fatalf("got %d responses, want %d", len(responses), len(lines))
	}
	checks := []string{"unknown argument", "missing required argument", "must not be empty", "Unknown tool", "arguments must be a JSON object"}
	for i, check := range checks {
		if !resultIsError(responses[i].Result) {
			t.Fatalf("response %d must be a tool error", i)
		}
		texts := contentTexts(t, responses[i].Result)
		if len(texts) == 0 || !strings.Contains(texts[0], check) {
			t.Fatalf("response %d = %v, want text containing %q", i, texts, check)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("API calls = %d, want 0 for invalid arguments", got)
	}
}
