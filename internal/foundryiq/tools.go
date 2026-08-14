package foundryiq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dcasati/mira/internal/telemetry"
)

type ToolRunner struct {
	client *Client
}

func NewToolRunner(client *Client) *ToolRunner {
	if client == nil {
		return nil
	}
	return &ToolRunner{client: client}
}

func (r *ToolRunner) Definitions() []telemetry.ToolDefinition {
	if r == nil {
		return nil
	}
	return []telemetry.ToolDefinition{
		{
			Type:        "function",
			Name:        "query_foundry_iq_manuals",
			Description: "Query Foundry IQ for answers grounded in uploaded manuals and Fabric IQ operational data. Use for manual/procedure questions and for Fabric IQ questions about missions, assets, events, call signs, prowords, relay hardline, checkpoints, or operational status.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "The worker's question to answer using Foundry IQ grounding. Preserve exact asset IDs, mission IDs, call signs, and requested source such as Fabric IQ.",
					},
				},
				"required":             []string{"question"},
				"additionalProperties": false,
			},
		},
	}
}

// Run answers a query_foundry_iq_manuals tool call by delegating the
// worker's question, verbatim, to the operator-persona-agent hosted agent.
// That agent owns its own routing between manuals and Fabric IQ
// operational data internally (via its instructions and grounding tools),
// so mira no longer needs to classify or rewrite the question itself.
func (r *ToolRunner) Run(ctx context.Context, name, arguments string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("Foundry IQ tools are not configured")
	}
	if name != "query_foundry_iq_manuals" {
		return "", fmt.Errorf("unknown Foundry IQ tool %q", name)
	}
	var args struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", err
	}
	queryCtx, cancel := TimeoutContext(ctx)
	defer cancel()
	result, err := r.client.Query(queryCtx, args.Question)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
