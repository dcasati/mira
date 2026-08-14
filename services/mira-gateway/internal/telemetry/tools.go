package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type ToolRunner struct {
	client *Client
}

type ToolDefinition struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func NewToolRunner(client *Client) *ToolRunner {
	if client == nil {
		return nil
	}
	return &ToolRunner{client: client}
}

func (r *ToolRunner) Definitions() []ToolDefinition {
	if r == nil {
		return nil
	}
	return []ToolDefinition{
		{
			Type:        "function",
			Name:        "get_device_telemetry",
			Description: "Get recent factory device telemetry rows from Fabric Real-Time Intelligence/Eventhouse. Use this for current device state, recent readings, alarms, and raw telemetry.",
			Parameters: objectSchema(map[string]any{
				"device_id": map[string]any{
					"type":        "string",
					"description": "Optional device identifier. Leave empty to see recent telemetry across devices.",
				},
				"lookback_minutes": map[string]any{
					"type":        "integer",
					"description": "How many minutes of history to query. Defaults to 15.",
					"minimum":     1,
					"maximum":     1440,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of rows to return. Defaults to the configured POC limit.",
					"minimum":     1,
					"maximum":     100,
				},
			}, []string{}),
		},
		{
			Type:        "function",
			Name:        "summarize_device_metric",
			Description: "Summarize one numeric telemetry metric from Fabric Real-Time Intelligence/Eventhouse with count, average, min, max, and latest timestamp.",
			Parameters: objectSchema(map[string]any{
				"metric": map[string]any{
					"type":        "string",
					"description": "Numeric telemetry column to summarize, for example temperature, pressure, vibration, rpm, or battery.",
				},
				"device_id": map[string]any{
					"type":        "string",
					"description": "Optional device identifier. Leave empty to summarize by device.",
				},
				"lookback_minutes": map[string]any{
					"type":        "integer",
					"description": "How many minutes of history to query. Defaults to 15.",
					"minimum":     1,
					"maximum":     1440,
				},
			}, []string{"metric"}),
		},
	}
}

func (r *ToolRunner) Run(ctx context.Context, name, arguments string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("telemetry tools are not configured")
	}
	switch name {
	case "get_device_telemetry":
		var args struct {
			DeviceID        string `json:"device_id"`
			LookbackMinutes int    `json:"lookback_minutes"`
			Limit           int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return "", err
		}
		result, err := r.client.Recent(ctx, args.DeviceID, minutes(args.LookbackMinutes), args.Limit)
		if err != nil {
			return "", err
		}
		return marshalToolResult(result)
	case "summarize_device_metric":
		var args struct {
			DeviceID        string `json:"device_id"`
			Metric          string `json:"metric"`
			LookbackMinutes int    `json:"lookback_minutes"`
		}
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return "", err
		}
		result, err := r.client.Summary(ctx, args.DeviceID, args.Metric, minutes(args.LookbackMinutes))
		if err != nil {
			return "", err
		}
		return marshalToolResult(result)
	default:
		return "", fmt.Errorf("unknown telemetry tool %q", name)
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func minutes(value int) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Minute
}

func marshalToolResult(result QueryResult) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
