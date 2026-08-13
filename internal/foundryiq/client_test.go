package foundryiq

import (
	"context"
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

func TestQueryCallsFoundryResponsesWithAgentReference(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"Use menu 7 to calibrate the device."}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/factory", AgentName: "manuals-agent"}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	client.cfg.ProjectEndpoint = server.URL
	result, err := client.Query(context.Background(), "How do I calibrate it?")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/openai/v1/responses" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"agent_reference"`) || !strings.Contains(gotBody, "manuals-agent") {
		t.Fatalf("body missing agent reference: %s", gotBody)
	}
	if result.Answer != "Use menu 7 to calibrate the device." {
		t.Fatalf("answer = %q", result.Answer)
	}
}

func TestConfigRequiresProjectAndAgent(t *testing.T) {
	_, err := NewClient(Config{ProjectEndpoint: "https://example.test"}, fakeCredential{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestQueryFabricKnowledgeBaseUsesDelegatedQuerySourceToken(t *testing.T) {
	var gotPath, gotQuerySourceAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuerySourceAuth = r.Header.Get("x-ms-query-source-authorization")
		_, _ = w.Write([]byte(`{"response":[{"content":[{"type":"text","text":"ASSET-004 has two events."}]}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		ProjectEndpoint:     "https://example.services.ai.azure.com/api/projects/factory",
		AgentName:           "manuals-agent",
		SearchEndpoint:      server.URL,
		SearchAPIKey:        "search-key",
		FabricKnowledgeBase: "ks-fabriciq-operator-matrix",
		QuerySourceToken:    "Bearer delegated-token",
	}, failingCredential{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(context.Background(), "What events are recorded for ASSET-004?")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/knowledgebases('ks-fabriciq-operator-matrix')/retrieve" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuerySourceAuth != "delegated-token" {
		t.Fatalf("query source auth = %q", gotQuerySourceAuth)
	}
	if result.Answer != "ASSET-004 has two events." {
		t.Fatalf("answer = %q", result.Answer)
	}
}

func TestQueryFallsBackToDirectFabricKnowledgeBaseWhenAgentCannotForwardQuerySourceAuth(t *testing.T) {
	var gotAgentPath, gotDirectPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/openai/v1/responses":
			gotAgentPath = r.URL.Path
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"No usable knowledge sources are available. Invalid header: 'x-ms-query-source-authorization' is invalid, null or empty."}}`))
		case strings.HasPrefix(r.URL.Path, "/knowledgebases("):
			gotDirectPath = r.URL.Path
			_, _ = w.Write([]byte(`{"response":[{"content":[{"type":"text","text":"Mission 1 is a relay hardline check."}]}]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		ProjectEndpoint:     "https://example.services.ai.azure.com/api/projects/factory",
		AgentName:           "manuals-agent",
		SearchEndpoint:      server.URL,
		SearchAPIKey:        "search-key",
		FabricKnowledgeBase: "ks-fabriciq-operator-matrix",
		QuerySourceToken:    "delegated-token",
	}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	client.cfg.ProjectEndpoint = server.URL
	result, err := client.Query(context.Background(), "How do I calibrate the operator handset?")
	if err != nil {
		t.Fatal(err)
	}
	if gotAgentPath != "/openai/v1/responses" {
		t.Fatalf("agent path = %q", gotAgentPath)
	}
	if gotDirectPath != "/knowledgebases('ks-fabriciq-operator-matrix')/retrieve" {
		t.Fatalf("direct path = %q", gotDirectPath)
	}
	if result.Answer != "Mission 1 is a relay hardline check." {
		t.Fatalf("answer = %q", result.Answer)
	}
}

func TestQueryRetriesAndReturnsErrorWhenFabricDataAgentFails(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/knowledgebases(") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"All retrieval tasks failed. Failures:\r\n Knowledge source 'ks-fabriciq-operator-matrix-v2': Failed to connect to Fabric Data Agent 'WorkspaceId: workspace, DataAgentId: agent' due to unexpected error. Please contact support if the issue persists"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		ProjectEndpoint:     "https://example.services.ai.azure.com/api/projects/factory",
		AgentName:           "manuals-agent",
		SearchEndpoint:      server.URL,
		SearchAPIKey:        "search-key",
		FabricKnowledgeBase: "ks-fabriciq-operator-matrix",
		QuerySourceToken:    "delegated-token",
	}, failingCredential{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), "What happened on the relay hardline?")
	if err == nil {
		t.Fatal("expected Fabric data agent error")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if !strings.Contains(err.Error(), "Failed to connect to Fabric Data Agent") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueryDoesNotFallbackForFabricAuthorizationErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid header: 'x-ms-query-source-authorization' is invalid, null or empty."}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		ProjectEndpoint:     "https://example.services.ai.azure.com/api/projects/factory",
		AgentName:           "manuals-agent",
		SearchEndpoint:      server.URL,
		SearchAPIKey:        "search-key",
		FabricKnowledgeBase: "ks-fabriciq-operator-matrix",
		QuerySourceToken:    "delegated-token",
	}, failingCredential{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), "What happened on the relay hardline?")
	if err == nil {
		t.Fatal("expected auth error")
	}
	if strings.Contains(err.Error(), "Fabric IQ POC fallback") {
		t.Fatalf("unexpected fallback error: %v", err)
	}
}

func TestRouteQuestionPrefersFabricIQForOperationalData(t *testing.T) {
	got := routeQuestion("What events are recorded for ASSET-004?")
	if got == "What events are recorded for ASSET-004?" {
		t.Fatal("expected operational question to be routed")
	}
	if !strings.Contains(got, "Using Fabric IQ/operator-matrix-agent data") || !strings.Contains(got, "ASSET-004") {
		t.Fatalf("routed question = %q", got)
	}
}

func TestRouteQuestionLeavesManualQuestionAlone(t *testing.T) {
	const q = "How do I configure split frequency on the FT-818ND?"
	if got := routeQuestion(q); got != q {
		t.Fatalf("routeQuestion() = %q, want %q", got, q)
	}
}

func TestRouteQuestionHandlesSpeechMisrecognition(t *testing.T) {
	got := routeQuestion("what events are we called for? Acid 004.")
	want := "Using Fabric IQ/operator-matrix-agent data, what events are recorded for ASSET-004?"
	if got != want {
		t.Fatalf("routed question = %q, want %q", got, want)
	}
}

func TestRouteQuestionCanonicalizesMissionOne(t *testing.T) {
	got := routeQuestion("what is mission one over?")
	want := "Using Fabric IQ/operator-matrix-agent data, what is Mission 1?"
	if got != want {
		t.Fatalf("routed question = %q, want %q", got, want)
	}
}
