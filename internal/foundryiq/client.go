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
	ProjectEndpoint string
	AgentName       string
	Model           string
	MaxOutputChars  int
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
	token, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{TokenScope}})
	if err != nil {
		return QueryResult{}, err
	}
	querySourceToken, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{QuerySourceTokenScope}})
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
	req.Header.Set("x-ms-query-source-authorization", querySourceToken.Token)
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
