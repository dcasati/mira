package foundryiq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// TokenScope is the Entra scope used to call the Foundry hosted agent's
// Responses API endpoint.
const TokenScope = "https://ai.azure.com/.default"

type contextKey string

const (
	sessionIDContextKey contextKey = "foundryiq.session_id"
	toolContextKey      contextKey = "foundryiq.tool"
)

// Config points mira at a Foundry hosted agent (operator-persona-agent)
// that answers grounded questions using its own knowledge sources.
//
// Previously mira queried Fabric IQ and Foundry IQ directly from this
// client, which required either a static Search API key + a delegated
// x-ms-query-source-authorization token (for the Fabric ontology knowledge
// source, which rejects app-only tokens by design) or falling back to a
// separate direct Fabric knowledge-base call when the Foundry agent
// couldn't forward that header. Both paths depended on a delegated user
// token that expires in ~1 hour and has no good headless-rotation story.
//
// operator-persona-agent (a Foundry hosted agent, see
// mira-operator-agent/src/operator-persona-agent/main.py) now owns all of
// that grounding logic itself: it calls kb-zavatrix via Azure AI Search MCP
// (api-key only) and reads Fabric operational data directly from OneLake,
// authenticated with its own managed identity -- no delegated token,
// anywhere, ever. mira only needs to call that hosted agent's Responses
// API with mira's existing workload identity (already granted "Foundry
// User" on the project), which is a plain app-only Entra bearer token with
// no rotation concerns.
//
// DirectEndpoint is an alternative to ProjectEndpoint/AgentName: when set,
// it points at a plain AKS-hosted agent (operator-agent-aks, the same
// agent_framework Agent code, just self-hosted instead of run through
// Foundry's Agent Service) reachable in-cluster with no auth at all (a
// ClusterIP Service, not exposed publicly). Live testing (2026-08-13)
// found the Foundry-hosted path adds a persistent ~16-20s of gateway
// overhead on top of the agent's own ~11.6s of actual work -- not present
// at all when calling operator-agent-aks directly. DirectEndpoint exists
// so mira-gateway (itself running in AKS) can skip that overhead
// entirely when both are deployed in the same cluster.
type Config struct {
	ProjectEndpoint string
	AgentName       string
	DirectEndpoint  string
	MaxOutputChars  int
}

func WithCallContext(ctx context.Context, sessionID, tool string) context.Context {
	ctx = context.WithValue(ctx, sessionIDContextKey, sessionID)
	return context.WithValue(ctx, toolContextKey, tool)
}

type Client struct {
	cfg    Config
	cred   azcore.TokenCredential
	client *http.Client
}

type QueryResult struct {
	Answer     string `json:"answer"`
	ResponseID string `json:"response_id,omitempty"`
	AgentName  string `json:"agent_name"`
}

type responseCreateRequest struct {
	Input string `json:"input"`
}

type responseCreateResponse struct {
	ID         string `json:"id"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func NewClient(cfg Config, cred azcore.TokenCredential) (*Client, error) {
	cfg.ProjectEndpoint = strings.TrimRight(strings.TrimSpace(cfg.ProjectEndpoint), "/")
	cfg.AgentName = strings.TrimSpace(cfg.AgentName)
	cfg.DirectEndpoint = strings.TrimRight(strings.TrimSpace(cfg.DirectEndpoint), "/")
	if cfg.MaxOutputChars <= 0 {
		cfg.MaxOutputChars = 6000
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, cred: cred, client: http.DefaultClient}, nil
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.ProjectEndpoint) != "" || strings.TrimSpace(c.AgentName) != "" || strings.TrimSpace(c.DirectEndpoint) != ""
}

func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	var errs []error
	if strings.TrimSpace(c.DirectEndpoint) != "" {
		if !strings.HasPrefix(c.DirectEndpoint, "http://") && !strings.HasPrefix(c.DirectEndpoint, "https://") {
			errs = append(errs, errors.New("FOUNDRY_IQ_DIRECT_ENDPOINT must use http:// or https://"))
		}
		return errors.Join(errs...)
	}
	if strings.TrimSpace(c.ProjectEndpoint) == "" {
		errs = append(errs, errors.New("FOUNDRY_PROJECT_ENDPOINT is required when Foundry IQ is enabled"))
	}
	if strings.TrimSpace(c.AgentName) == "" {
		errs = append(errs, errors.New("FOUNDRY_IQ_AGENT_NAME is required when Foundry IQ is enabled"))
	}
	if c.ProjectEndpoint != "" && !strings.HasPrefix(c.ProjectEndpoint, "https://") {
		errs = append(errs, errors.New("FOUNDRY_PROJECT_ENDPOINT must use https://"))
	}
	return errors.Join(errs...)
}

// Query asks the operator agent (either the Foundry-hosted
// operator-persona-agent, or -- when DirectEndpoint is set -- the
// self-hosted operator-agent-aks reachable in-cluster) a question and
// returns its grounded, persona-styled answer. The agent decides
// internally which of its own knowledge sources (radio manuals vs. Fabric
// operational data) to use; mira does not need to route or rewrite the
// question itself.
func (c *Client) Query(ctx context.Context, question string) (QueryResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return QueryResult{}, errors.New("question is required")
	}

	body, err := json.Marshal(responseCreateRequest{Input: question})
	if err != nil {
		return QueryResult{}, err
	}

	direct := c.cfg.DirectEndpoint != ""
	var endpoint, agentLabel string
	if direct {
		endpoint = c.cfg.DirectEndpoint + "/responses"
		agentLabel = "operator-agent-aks"
	} else {
		endpoint = fmt.Sprintf("%s/agents/%s/endpoint/protocols/openai/responses?api-version=v1", c.cfg.ProjectEndpoint, c.cfg.AgentName)
		agentLabel = c.cfg.AgentName
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return QueryResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if !direct {
		// operator-agent-aks is a plain ClusterIP Service with no auth of
		// its own (not exposed publicly); the Foundry-hosted path still
		// needs a bearer token scoped for its Responses API.
		token, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{TokenScope}})
		if err != nil {
			return QueryResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token.Token)
	}

	start := time.Now()
	logQuery(ctx, slog.LevelInfo, "foundry_iq.query_started",
		"agent_name", agentLabel,
		"http_endpoint", endpoint,
		"direct", direct,
	)
	resp, err := c.client.Do(req)
	if err != nil {
		logQuery(ctx, slog.LevelWarn, "foundry_iq.query_failed",
			"agent_name", agentLabel,
			"elapsed_ms", time.Since(start).Milliseconds(),
			"error", err.Error(),
		)
		return QueryResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return QueryResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		logQuery(ctx, slog.LevelWarn, "foundry_iq.query_failed",
			"agent_name", agentLabel,
			"foundry_request_id", resp.Header.Get("x-request-id"),
			"http_status", resp.StatusCode,
			"elapsed_ms", time.Since(start).Milliseconds(),
		)
		return QueryResult{}, fmt.Errorf("Foundry IQ query failed: status=%d request_id=%s body=%s", resp.StatusCode, resp.Header.Get("x-request-id"), strings.TrimSpace(string(data)))
	}
	var parsed responseCreateResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return QueryResult{}, err
	}
	answer := strings.TrimSpace(parsed.OutputText)
	if answer == "" {
		answer = extractOutputText(parsed)
	}
	if c.cfg.MaxOutputChars > 0 && len(answer) > c.cfg.MaxOutputChars {
		answer = answer[:c.cfg.MaxOutputChars] + "..."
	}
	logQuery(ctx, slog.LevelInfo, "foundry_iq.query_finished",
		"agent_name", agentLabel,
		"foundry_request_id", resp.Header.Get("x-request-id"),
		"http_status", resp.StatusCode,
		"elapsed_ms", time.Since(start).Milliseconds(),
	)
	return QueryResult{Answer: answer, ResponseID: parsed.ID, AgentName: agentLabel}, nil
}

func extractOutputText(resp responseCreateResponse) string {
	var out strings.Builder
	for _, item := range resp.Output {
		for _, content := range item.Content {
			out.WriteString(content.Text)
		}
	}
	return strings.TrimSpace(out.String())
}

func logQuery(ctx context.Context, level slog.Level, message string, args ...any) {
	base := []any{
		"session_id", contextString(ctx, sessionIDContextKey),
		"tool", contextString(ctx, toolContextKey),
	}
	slog.Log(ctx, level, message, append(base, args...)...)
}

func contextString(ctx context.Context, key contextKey) string {
	value, _ := ctx.Value(key).(string)
	return value
}

// TimeoutContext bounds a single query_foundry_iq_manuals tool call, which
// is now a single HTTP request to the operator-persona-agent hosted agent.
// That hosted agent may itself invoke one grounding tool (kb-zavatrix or
// the Fabric operator-matrix-agent Data Agent) before replying; observed
// end-to-end latency for that has ranged from a few seconds up to ~40s.
// 100s leaves comfortable headroom without making a live radio caller
// wait an excessive amount if the hosted agent or one of its tools is slow.
func TimeoutContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 100*time.Second)
}
