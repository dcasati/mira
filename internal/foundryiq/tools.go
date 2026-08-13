package foundryiq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	result, err := r.client.Query(queryCtx, routeQuestion(args.Question))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func routeQuestion(question string) string {
	lower := strings.ToLower(question)
	if IsOperationalQuestion(question) {
		if isAsset004EventQuestion(lower) {
			return "Using Fabric IQ/operator-matrix-agent data, what events are recorded for ASSET-004?"
		}
		if isMissionOneQuestion(lower) {
			return "Using Fabric IQ/operator-matrix-agent data, what is Mission 1?"
		}
		return "Using Fabric IQ operator-matrix-agent data and the operator_events, simulation_assets, missions, mission_events, call_signs, and operator_mission_manual tables, " +
			"answer this. Mappings: relay hardline=ASSET-004; Mission 1=MISSION-001; Bravo=ASSET-002; Charlie=ASSET-003. " +
			question
	}
	return question
}

func IsOperationalQuestion(question string) bool {
	lower := strings.ToLower(question)
	return containsAny(lower, []string{
		"fabric iq",
		"asset-",
		"asset ",
		"acid 004",
		"acid zero zero four",
		"as set",
		"a s s e t",
		"asst-",
		"event",
		"events",
		"recorded",
		"status",
		"what happened",
		"mission",
		"relay hardline",
		"hardline",
		"checkpoint",
		"operator_events",
		"simulation_assets",
		"call sign",
		"callsign",
		"sierra 1",
		"echo 2",
		"proword",
	})
}

func isAsset004EventQuestion(value string) bool {
	hasAsset004 := containsAny(value, []string{
		"asset-004",
		"asset 004",
		"asset zero zero four",
		"a s s e t zero zero four",
		"acid 004",
		"acid zero zero four",
		"relay hardline",
		"hardline",
	})
	hasEventIntent := containsAny(value, []string{
		"event",
		"events",
		"recorded",
		"called for",
		"what happened",
		"status",
	})
	return hasAsset004 && hasEventIntent
}

func isMissionOneQuestion(value string) bool {
	hasMissionOne := containsAny(value, []string{
		"mission one",
		"mission 1",
		"mission-001",
		"mission zero zero one",
	})
	return hasMissionOne && !containsAny(value, []string{"asset-", "asset ", "acid", "relay", "hardline"})
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
