package foundryiq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

const (
	TokenScope            = "https://ai.azure.com/.default"
	QuerySourceTokenScope = "https://search.azure.com/.default"
)

type Config struct {
	ProjectEndpoint     string
	AgentName           string
	Model               string
	MaxOutputChars      int
	SearchEndpoint      string
	SearchAPIKey        string
	FabricKnowledgeBase string
	QuerySourceToken    string
}

type Client struct {
	cfg    Config
	cred   azcore.TokenCredential
	client *http.Client
}

type knowledgeRetrieveRequest struct {
	Messages                 []knowledgeMessage `json:"messages"`
	MaxRuntimeInSeconds      int                `json:"maxRuntimeInSeconds"`
	MaxOutputSize            int                `json:"maxOutputSize"`
	MaxOutputDocuments       int                `json:"maxOutputDocuments"`
	RetrievalReasoningEffort map[string]string  `json:"retrievalReasoningEffort"`
	IncludeActivity          bool               `json:"includeActivity"`
	OutputMode               string             `json:"outputMode"`
}

type knowledgeMessage struct {
	Role    string                 `json:"role"`
	Content []knowledgeTextContent `json:"content"`
}

type knowledgeTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type knowledgeRetrieveResponse struct {
	Response []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type QueryResult struct {
	Answer     string `json:"answer"`
	ResponseID string `json:"response_id,omitempty"`
	AgentName  string `json:"agent_name"`
}

func (c *Client) directFabricEnabled() bool {
	return c.cfg.SearchEndpoint != "" && c.cfg.SearchAPIKey != "" && c.cfg.FabricKnowledgeBase != ""
}

func (c *Client) queryFabricKnowledgeBase(ctx context.Context, question string) (QueryResult, error) {
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		result, err := c.queryFabricKnowledgeBaseOnce(ctx, question)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isFabricDataAgentTransient(err) || attempt == 5 {
			break
		}
		delay := time.Duration(attempt) * 3 * time.Second
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return QueryResult{}, ctx.Err()
		}
	}
	return QueryResult{}, lastErr
}

func (c *Client) queryFabricKnowledgeBaseOnce(ctx context.Context, question string) (QueryResult, error) {
	querySourceAuth, err := c.querySourceAuthorization(ctx)
	if err != nil {
		return QueryResult{}, err
	}
	body, err := json.Marshal(knowledgeRetrieveRequest{
		Messages: []knowledgeMessage{{
			Role: "user",
			Content: []knowledgeTextContent{{
				Type: "text",
				Text: question,
			}},
		}},
		MaxRuntimeInSeconds:      60,
		MaxOutputSize:            100000,
		MaxOutputDocuments:       10,
		RetrievalReasoningEffort: map[string]string{"kind": "low"},
		IncludeActivity:          true,
		OutputMode:               "answerSynthesis",
	})
	if err != nil {
		return QueryResult{}, err
	}
	endpoint := fmt.Sprintf("%s/knowledgebases('%s')/retrieve?api-version=2026-05-01-preview", c.cfg.SearchEndpoint, c.cfg.FabricKnowledgeBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return QueryResult{}, err
	}
	req.Header.Set("api-key", c.cfg.SearchAPIKey)
	req.Header.Set("x-ms-query-source-authorization", querySourceAuth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return QueryResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return QueryResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return QueryResult{}, fmt.Errorf("Fabric IQ direct query failed: status=%d request_id=%s body=%s", resp.StatusCode, resp.Header.Get("request-id"), strings.TrimSpace(string(data)))
	}
	var parsed knowledgeRetrieveResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return QueryResult{}, err
	}
	if parsed.Error != nil {
		return QueryResult{}, fmt.Errorf("Fabric IQ direct query failed: %s", parsed.Error.Message)
	}
	answer := extractKnowledgeResponseText(parsed)
	if c.cfg.MaxOutputChars > 0 && len(answer) > c.cfg.MaxOutputChars {
		answer = answer[:c.cfg.MaxOutputChars] + "..."
	}
	return QueryResult{Answer: answer, AgentName: c.cfg.AgentName}, nil
}

func extractKnowledgeResponseText(resp knowledgeRetrieveResponse) string {
	var out strings.Builder
	for _, item := range resp.Response {
		for _, content := range item.Content {
			out.WriteString(content.Text)
		}
	}
	return strings.TrimSpace(out.String())
}

type responseCreateRequest struct {
	Input          string         `json:"input"`
	Model          string         `json:"model,omitempty"`
	AgentReference map[string]any `json:"agent_reference,omitempty"`
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
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.SearchEndpoint = strings.TrimRight(strings.TrimSpace(cfg.SearchEndpoint), "/")
	cfg.SearchAPIKey = strings.TrimSpace(cfg.SearchAPIKey)
	cfg.FabricKnowledgeBase = strings.TrimSpace(cfg.FabricKnowledgeBase)
	cfg.QuerySourceToken = normalizeQuerySourceAuthorization(cfg.QuerySourceToken)
	if cfg.MaxOutputChars <= 0 {
		cfg.MaxOutputChars = 6000
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, cred: cred, client: http.DefaultClient}, nil
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.ProjectEndpoint) != "" || strings.TrimSpace(c.AgentName) != ""
}

func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	var errs []error
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

func (c *Client) Query(ctx context.Context, question string) (QueryResult, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return QueryResult{}, errors.New("question is required")
	}
	if c.directFabricEnabled() && IsOperationalQuestion(question) {
		routed := routeQuestion(question)
		return c.queryFabricKnowledgeBase(ctx, routed)
	}
	result, err := c.queryFoundryAgent(ctx, question)
	if err != nil && c.directFabricEnabled() && isQuerySourceAuthorizationBlocked(err) {
		routed := routeQuestion(question)
		return c.queryFabricKnowledgeBase(ctx, routed)
	}
	return result, err
}

func (c *Client) queryFoundryAgent(ctx context.Context, question string) (QueryResult, error) {
	token, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{TokenScope}})
	if err != nil {
		return QueryResult{}, err
	}
	querySourceAuth, err := c.querySourceAuthorization(ctx)
	if err != nil {
		return QueryResult{}, err
	}
	body, err := json.Marshal(responseCreateRequest{
		Input: question,
		Model: c.cfg.Model,
		AgentReference: map[string]any{
			"type": "agent_reference",
			"name": c.cfg.AgentName,
		},
	})
	if err != nil {
		return QueryResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.ProjectEndpoint+"/openai/v1/responses", bytes.NewReader(body))
	if err != nil {
		return QueryResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("x-ms-query-source-authorization", querySourceAuth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return QueryResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return QueryResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
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
	return QueryResult{Answer: answer, ResponseID: parsed.ID, AgentName: c.cfg.AgentName}, nil
}

func isQuerySourceAuthorizationBlocked(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "x-ms-query-source-authorization") &&
		(strings.Contains(msg, "invalid, null or empty") || strings.Contains(msg, "no usable knowledge sources"))
}

func isFabricDataAgentTransient(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fabric data agent") &&
		(strings.Contains(msg, "status=502") ||
			strings.Contains(msg, "status=503") ||
			strings.Contains(msg, "status=429") ||
			strings.Contains(msg, "failed to connect") ||
			strings.Contains(msg, "unexpected error") ||
			strings.Contains(msg, "all retrieval tasks failed"))
}

func (c *Client) querySourceAuthorization(ctx context.Context) (string, error) {
	if c.cfg.QuerySourceToken != "" {
		return c.cfg.QuerySourceToken, nil
	}
	querySourceToken, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{QuerySourceTokenScope}})
	if err != nil {
		return "", err
	}
	return querySourceToken.Token, nil
}

func normalizeQuerySourceAuthorization(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[len("bearer "):])
	}
	return value
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

func TimeoutContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 45*time.Second)
}
