package foundryiq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

type fakeCredential struct{}

func (fakeCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

type failingCredential struct{}

func (failingCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	panic("credential should not be used")
}

func TestQueryCallsHostedAgentResponsesEndpoint(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"Use menu 7 to calibrate the device."}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/factory", AgentName: "operator-persona-agent"}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	client.cfg.ProjectEndpoint = server.URL
	result, err := client.Query(context.Background(), "How do I calibrate it?")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/agents/operator-persona-agent/endpoint/protocols/openai/responses" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	var sentBody struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal([]byte(gotBody), &sentBody); err != nil {
		t.Fatalf("body not valid JSON: %s", gotBody)
	}
	if sentBody.Input != "How do I calibrate it?" {
		t.Fatalf("input = %q", sentBody.Input)
	}
	if result.Answer != "Use menu 7 to calibrate the device." {
		t.Fatalf("answer = %q", result.Answer)
	}
	if result.AgentName != "operator-persona-agent" {
		t.Fatalf("agent name = %q", result.AgentName)
	}
}

func TestQueryFallsBackToOutputContentWhenOutputTextMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"resp_2","output":[{"content":[{"type":"output_text","text":"ASSET-004 has two events."}]}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/factory", AgentName: "operator-persona-agent"}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	client.cfg.ProjectEndpoint = server.URL
	result, err := client.Query(context.Background(), "What events are recorded for ASSET-004?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "ASSET-004 has two events." {
		t.Fatalf("answer = %q", result.Answer)
	}
}

func TestQueryDoesNotRetryOnFailureAndReturnsError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"backend unavailable"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/factory", AgentName: "operator-persona-agent"}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	client.cfg.ProjectEndpoint = server.URL
	_, err = client.Query(context.Background(), "What happened on the relay hardline?")
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (single attempt, no retry)", calls)
	}
	if !strings.Contains(err.Error(), "status=502") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueryRequiresNonEmptyQuestion(t *testing.T) {
	client, err := NewClient(Config{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/factory", AgentName: "operator-persona-agent"}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestConfigRequiresProjectEndpointAndAgentName(t *testing.T) {
	if _, err := NewClient(Config{ProjectEndpoint: "https://example.test"}, fakeCredential{}); err == nil {
		t.Fatal("expected validation error when agent name missing")
	}
	if _, err := NewClient(Config{AgentName: "operator-persona-agent"}, fakeCredential{}); err == nil {
		t.Fatal("expected validation error when project endpoint missing")
	}
	if _, err := NewClient(Config{ProjectEndpoint: "http://example.test", AgentName: "operator-persona-agent"}, fakeCredential{}); err == nil {
		t.Fatal("expected validation error for non-https project endpoint")
	}
}

func TestConfigDisabledWhenEmpty(t *testing.T) {
	if (Config{}).Enabled() {
		t.Fatal("expected empty config to be disabled")
	}
	if (Config{}).Validate() != nil {
		t.Fatal("expected empty (disabled) config to validate cleanly")
	}
}

func TestQueryCallsDirectEndpointWithoutAuth(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"Sierra 1 is a forward field unit."}`))
	}))
	defer server.Close()

	// failingCredential panics if GetToken is ever called -- proves the
	// direct (in-cluster, no-auth) path never touches the credential.
	client, err := NewClient(Config{DirectEndpoint: server.URL}, failingCredential{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(context.Background(), "I need information on the callsign Sierra 1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/responses" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "" {
		t.Fatalf("auth = %q, want no Authorization header for direct endpoint", gotAuth)
	}
	var sentBody struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal([]byte(gotBody), &sentBody); err != nil {
		t.Fatalf("body not valid JSON: %s", gotBody)
	}
	if sentBody.Input != "I need information on the callsign Sierra 1" {
		t.Fatalf("input = %q", sentBody.Input)
	}
	if result.Answer != "Sierra 1 is a forward field unit." {
		t.Fatalf("answer = %q", result.Answer)
	}
	if result.AgentName != "operator-agent-aks" {
		t.Fatalf("agent name = %q", result.AgentName)
	}
}

func TestConfigDirectEndpointDoesNotRequireProjectOrAgentName(t *testing.T) {
	if _, err := NewClient(Config{DirectEndpoint: "http://operator-agent-aks.operator-agent-aks.svc.cluster.local:8088"}, failingCredential{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigDirectEndpointRequiresScheme(t *testing.T) {
	if _, err := NewClient(Config{DirectEndpoint: "operator-agent-aks.operator-agent-aks.svc.cluster.local:8088"}, failingCredential{}); err == nil {
		t.Fatal("expected validation error for direct endpoint missing http(s):// scheme")
	}
}
