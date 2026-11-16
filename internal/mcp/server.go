// Package mcp implements the Model Context Protocol (MCP) stdio transport and
// the Unkindled workstation tools exposed by this server.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/arthuRHD/unkindled-mcp/internal/api"
)

// Version is the server version reported during initialize. It can be
// overridden at build time with -ldflags.
var Version = "1.2.0"

// protocolVersion is the MCP revision implemented by this server.
const protocolVersion = "2024-11-05"

const initializeInstructions = "Manage Unkindled workstations: list and inspect workstations, " +
	"read connection details, start/stop/restart them, create and restore checkpoints, " +
	"list events, and delete workstations. Destructive tools require confirm=true."

const missingTokenMessage = "Configuration error: UNKINDLED_API_TOKEN is not set. " +
	"Add it to the MCP server environment; no request was sent to Unkindled."

// Server handles newline-delimited JSON-RPC 2.0 requests on a reader/writer
// pair. It never writes anything except protocol responses to the writer.
type Server struct {
	api             *api.Client
	tokenConfigured bool
}

// NewServer returns a server backed by client. tokenConfigured controls whether
// tool calls are allowed; when false, the server still initializes normally.
func NewServer(client *api.Client, tokenConfigured bool) *Server {
	return &Server{api: client, tokenConfigured: tokenConfigured}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

var nullID = json.RawMessage("null")

// Serve processes JSON-RPC messages until the input reaches EOF.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if line != "" && strings.TrimSpace(line) != "" {
			response, send := s.handleLine(ctx, line)
			if send {
				encoder := json.NewEncoder(writer)
				encoder.SetEscapeHTML(false)
				if writeErr := encoder.Encode(response); writeErr != nil {
					return fmt.Errorf("write response: %w", writeErr)
				}
				if flushErr := writer.Flush(); flushErr != nil {
					return fmt.Errorf("flush response: %w", flushErr)
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}
	}
}

func (s *Server) handleLine(ctx context.Context, line string) (response *rpcResponse, send bool) {
	defer func() {
		if recover() != nil {
			response = newErrorResponse(nullID, -32603, "Internal error")
			send = true
		}
	}()

	trimmed := strings.TrimSpace(line)
	if !json.Valid([]byte(trimmed)) {
		return newErrorResponse(nullID, -32700, "Parse error: invalid JSON"), true
	}
	if strings.HasPrefix(trimmed, "[") {
		return newErrorResponse(nullID, -32600, "Invalid Request: batch requests are not supported"), true
	}

	var req rpcRequest
	if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
		return newErrorResponse(nullID, -32600, "Invalid Request: expected a single JSON-RPC object"), true
	}

	hasID := len(req.ID) > 0
	if hasID && !validRequestID(req.ID) {
		return newErrorResponse(nullID, -32600, "Invalid Request: id must be a string, number, or null"), true
	}

	id := nullID
	if hasID {
		id = req.ID
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return newErrorResponse(id, -32600, "Invalid Request: jsonrpc must be \"2.0\" and method is required"), true
	}

	switch req.Method {
	case "initialize":
		if !hasID {
			return nil, false
		}
		if err := validateOptionalObject(req.Params); err != nil {
			return newErrorResponse(id, -32602, "Invalid params: "+err.Error()), true
		}
		result := map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "unkindled-mcp",
				"version": Version,
			},
			"instructions": initializeInstructions,
		}
		return newResultResponse(id, result), true

	case "notifications/initialized":
		return nil, false

	case "ping":
		if !hasID {
			return nil, false
		}
		return newResultResponse(id, json.RawMessage(`{}`)), true

	case "tools/list":
		if !hasID {
			return nil, false
		}
		if err := validateOptionalObject(req.Params); err != nil {
			return newErrorResponse(id, -32602, "Invalid params: "+err.Error()), true
		}
		return newResultResponse(id, map[string]any{"tools": toolList()}), true

	case "tools/call":
		if !hasID {
			return nil, false
		}
		return newResultResponse(id, s.callTool(ctx, req.Params)), true

	default:
		if !hasID {
			return nil, false
		}
		return newErrorResponse(id, -32601, "Method not found: "+req.Method), true
	}
}

func (s *Server) callTool(ctx context.Context, rawParams json.RawMessage) (result any) {
	defer func() {
		if recover() != nil {
			result = toolError("Internal error while running the tool")
		}
	}()

	if len(bytes.TrimSpace(rawParams)) == 0 {
		return toolError("Invalid params: tools/call requires a name and arguments object")
	}

	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return toolError("Invalid params: tools/call requires a name and arguments object")
	}
	if params.Name == "" {
		return toolError("Invalid params: tool name is required")
	}

	arguments := map[string]any{}
	if raw := bytes.TrimSpace(params.Arguments); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&arguments); err != nil {
			return toolError("Invalid params: arguments must be a JSON object")
		}
	}

	tool := findTool(params.Name)
	if tool == nil {
		return toolError(fmt.Sprintf("Unknown tool %q", params.Name))
	}
	if !s.tokenConfigured {
		return toolError(missingTokenMessage)
	}
	if err := validateArguments(tool.InputSchema, arguments); err != nil {
		return toolError(fmt.Sprintf("Invalid arguments for %s: %s", tool.Name, err))
	}

	output, err := tool.Run(ctx, s.api, arguments)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			return toolError(apiErr.Error())
		}
		return toolError("Error: " + err.Error())
	}

	content := []contentBlock{{Type: "text", Text: output.Summary}}
	if output.Data != nil {
		encoded, err := json.Marshal(output.Data)
		if err != nil {
			return toolError("Error: could not encode tool result")
		}
		content = append(content, contentBlock{Type: "text", Text: string(encoded)})
	}
	return callToolResult{Content: content}
}

func toolError(message string) callToolResult {
	return callToolResult{
		Content: []contentBlock{{Type: "text", Text: message}},
		IsError: true,
	}
}

func validateOptionalObject(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return fmt.Errorf("params must be an object")
	}
	return nil
}

func validRequestID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func newResultResponse(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func newErrorResponse(id json.RawMessage, code int, message string) *rpcResponse {
	if len(id) == 0 {
		id = nullID
	}
	return &rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
}

// validateArguments performs the flat-object validation needed by the tool
// schemas in this server: required fields, unknown fields, and scalar types.
func validateArguments(schema map[string]any, arguments map[string]any) error {
	properties, _ := schema["properties"].(map[string]any)
	required := requiredFields(schema)

	for _, name := range required {
		if _, ok := arguments[name]; !ok {
			return fmt.Errorf("missing required argument %q", name)
		}
	}

	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		value := arguments[name]
		propertyValue, ok := properties[name]
		if !ok {
			allowed := make([]string, 0, len(properties))
			for allowedName := range properties {
				allowed = append(allowed, allowedName)
			}
			sort.Strings(allowed)
			return fmt.Errorf("unknown argument %q (allowed: %s)", name, strings.Join(allowed, ", "))
		}
		property, _ := propertyValue.(map[string]any)
		if err := validateValue(name, value, property); err != nil {
			return err
		}
	}
	return nil
}

func requiredFields(schema map[string]any) []string {
	switch required := schema["required"].(type) {
	case []string:
		return required
	case []any:
		out := make([]string, 0, len(required))
		for _, entry := range required {
			if name, ok := entry.(string); ok {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}

func validateValue(name string, value any, property map[string]any) error {
	if value == nil {
		return fmt.Errorf("argument %q must not be null", name)
	}

	expected, _ := property["type"].(string)
	switch expected {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("argument %q must be a string", name)
		}
		if minLength, ok := property["minLength"].(int); ok && len([]rune(text)) < minLength {
			return fmt.Errorf("argument %q must not be empty", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("argument %q must be a boolean", name)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("argument %q must be an integer", name)
		}
		if _, err := strconv.ParseInt(number.String(), 10, 64); err != nil {
			return fmt.Errorf("argument %q must be an integer", name)
		}
	case "number":
		switch number := value.(type) {
		case json.Number:
			if _, err := number.Float64(); err != nil {
				return fmt.Errorf("argument %q must be a number", name)
			}
		case float64:
			if math.IsNaN(number) || math.IsInf(number, 0) {
				return fmt.Errorf("argument %q must be a number", name)
			}
		default:
			return fmt.Errorf("argument %q must be a number", name)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("argument %q must be an object", name)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("argument %q must be an array", name)
		}
	}
	return nil
}
