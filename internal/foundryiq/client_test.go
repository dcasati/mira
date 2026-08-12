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
