package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/arthuRHD/unkindled-mcp/internal/api"
)

// Tool is an MCP tool backed by the Unkindled API.
type Tool struct {
	Name        string                                                                 `json:"name"`
	Description string                                                                 `json:"description"`
	InputSchema map[string]any                                                         `json:"inputSchema"`
	Run         func(context.Context, *api.Client, map[string]any) (ToolResult, error) `json:"-"`
}

// ToolResult is the formatted result of a successful tool invocation.
type ToolResult struct {
	Summary string
	Data    any
}

func toolList() []Tool {
	return []Tool{
		{
			Name:        "list_workstations",
			Description: "List Unkindled workstations visible to the authenticated account. Optionally include soft-deleted workstations.",
			InputSchema: schema(map[string]any{
				"include_deleted": boolProperty("Include soft-deleted workstations. Defaults to false."),
			}),
			Run: listWorkstations,
		},
		{
			Name:        "get_workstation",
			Description: "Get details for a single Unkindled workstation by ID.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: getWorkstation,
		},
		{
			Name:        "get_connection",
			Description: "Get SSH and Herdr connection details for a workstation. Private keys and provider secrets are never returned.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: getConnection,
		},
		{
			Name:        "start_workstation",
			Description: "Start a stopped Unkindled workstation. The API may complete the operation asynchronously.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: startWorkstation,
		},
		{
			Name:        "stop_workstation",
			Description: "Stop a running Unkindled workstation. The API may complete the operation asynchronously.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: stopWorkstation,
		},
		{
			Name:        "restart_workstation",
			Description: "Restart an Unkindled workstation. The API may complete the operation asynchronously.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: restartWorkstation,
		},
		{
			Name:        "create_checkpoint",
			Description: "Create a checkpoint for a workstation, optionally with a short label and reason.",
			InputSchema: schema(map[string]any{
				"id":     idProperty("Workstation ID."),
				"label":  stringProperty("Optional short checkpoint label."),
				"reason": stringProperty("Optional reason for creating the checkpoint."),
			}, "id"),
			Run: createCheckpoint,
		},
		{
			Name:        "list_checkpoints",
			Description: "List checkpoints for a workstation.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: listCheckpoints,
		},
		{
			Name:        "restore_checkpoint",
			Description: "Destructively restore a workstation to a checkpoint. Requires confirm=true and discards changes made after the checkpoint.",
			InputSchema: schema(map[string]any{
				"id":            idProperty("Workstation ID."),
				"checkpoint_id": idProperty("Checkpoint ID to restore."),
				"confirm":       confirmProperty("Must be true to perform the restore. If false, nothing is done."),
			}, "id", "checkpoint_id", "confirm"),
			Run: restoreCheckpoint,
		},
		{
			Name:        "delete_workstation",
			Description: "Destructively delete a workstation. Requires confirm=true. This cannot be undone unless the provider retains the resource.",
			InputSchema: schema(map[string]any{
				"id":      idProperty("Workstation ID."),
				"confirm": confirmProperty("Must be true to delete the workstation. If false, nothing is done."),
			}, "id", "confirm"),
			Run: deleteWorkstation,
		},
		{
			Name:        "list_events",
			Description: "List recent events for a workstation, such as lifecycle and checkpoint activity.",
			InputSchema: schema(map[string]any{
				"id": idProperty("Workstation ID."),
			}, "id"),
			Run: listEvents,
		},
	}
}

func findTool(name string) *Tool {
	tools := toolList()
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func schema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func idProperty(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"minLength":   1,
		"description": description,
	}
}

func stringProperty(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func boolProperty(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

func confirmProperty(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

func listWorkstations(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	path := "/v1/workstations"
	if includeDeleted, ok := args["include_deleted"].(bool); ok {
		path += "?" + url.Values{
			"include_deleted": []string{strconv.FormatBool(includeDeleted)},
		}.Encode()
	}
	return fetch(ctx, client, http.MethodGet, path, nil, "Listed workstations")
}

func getWorkstation(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodGet, workstationPath(id), nil,
		fmt.Sprintf("Fetched workstation %q", id))
}

func getConnection(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodGet, workstationPath(id)+"/connection", nil,
		fmt.Sprintf("Fetched connection details for workstation %q; private keys and provider secrets are not exposed", id))
}

func startWorkstation(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodPost, workstationPath(id)+"/start", nil,
		fmt.Sprintf("Start requested for workstation %q", id))
}

func stopWorkstation(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodPost, workstationPath(id)+"/stop", nil,
		fmt.Sprintf("Stop requested for workstation %q", id))
}

func restartWorkstation(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodPost, workstationPath(id)+"/restart", nil,
		fmt.Sprintf("Restart requested for workstation %q", id))
}

func createCheckpoint(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	body := map[string]any{}
	if label, ok := args["label"].(string); ok {
		body["label"] = label
	}
	if reason, ok := args["reason"].(string); ok {
		body["reason"] = reason
	}

	summary := fmt.Sprintf("Created checkpoint for workstation %q", id)
	if label, ok := args["label"].(string); ok && label != "" {
		summary = fmt.Sprintf("Created checkpoint %q for workstation %q", label, id)
	}
	return fetch(ctx, client, http.MethodPost, workstationPath(id)+"/checkpoints", body, summary)
}

func listCheckpoints(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodGet, workstationPath(id)+"/checkpoints", nil,
		fmt.Sprintf("Listed checkpoints for workstation %q", id))
}

func restoreCheckpoint(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	checkpointID := args["checkpoint_id"].(string)
	if !args["confirm"].(bool) {
		return ToolResult{
			Summary: fmt.Sprintf("Nothing was done: restoring checkpoint %q for workstation %q requires \"confirm\": true.", checkpointID, id),
			Data: map[string]any{
				"performed":      false,
				"workstation_id": id,
				"checkpoint_id":  checkpointID,
				"reason":         "confirm was not true",
			},
		}, nil
	}
	return fetch(ctx, client, http.MethodPost,
		workstationPath(id)+"/checkpoints/"+url.PathEscape(checkpointID)+"/restore", nil,
		fmt.Sprintf("Restored checkpoint %q for workstation %q", checkpointID, id))
}

func deleteWorkstation(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	if !args["confirm"].(bool) {
		return ToolResult{
			Summary: fmt.Sprintf("Nothing was done: deleting workstation %q requires \"confirm\": true.", id),
			Data: map[string]any{
				"performed":      false,
				"workstation_id": id,
				"reason":         "confirm was not true",
			},
		}, nil
	}
	return fetch(ctx, client, http.MethodDelete, workstationPath(id), nil,
		fmt.Sprintf("Deleted workstation %q", id))
}

func listEvents(ctx context.Context, client *api.Client, args map[string]any) (ToolResult, error) {
	id := args["id"].(string)
	return fetch(ctx, client, http.MethodGet, workstationPath(id)+"/events", nil,
		fmt.Sprintf("Listed events for workstation %q", id))
}

func workstationPath(id string) string {
	return "/v1/workstations/" + url.PathEscape(id)
}

func fetch(ctx context.Context, client *api.Client, method, path string, body any, summary string) (ToolResult, error) {
	response, err := client.Do(ctx, method, path, body)
	if err != nil {
		return ToolResult{}, err
	}

	data := client.Decode(response.Body)
	if data == nil {
		data = map[string]any{"status_code": response.StatusCode}
	}

	return ToolResult{
		Summary: fmt.Sprintf("%s (HTTP %d %s).", summary, response.StatusCode, statusText(response.StatusCode)),
		Data:    data,
	}, nil
}

func statusText(statusCode int) string {
	switch statusCode {
	case http.StatusAccepted:
		return "Accepted, pending"
	case http.StatusNoContent:
		return "No Content"
	}
	if text := http.StatusText(statusCode); text != "" {
		return text
	}
	return fmt.Sprintf("status %d", statusCode)
}
