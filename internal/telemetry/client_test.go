package telemetry

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

func TestRecentQueriesKustoEndpoint(t *testing.T) {
	var gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		_, _ = w.Write([]byte(`{"Tables":[{"TableName":"PrimaryResult","Columns":[{"ColumnName":"DeviceId","DataType":"String"},{"ColumnName":"temperature","DataType":"Real"}],"Rows":[["pump-1",72.5]]}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Endpoint:     server.URL,
		Database:     "Factory",
		Table:        "Telemetry",
		TimeColumn:   "Timestamp",
		DeviceColumn: "DeviceId",
	}, fakeCredential{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Recent(context.Background(), "pump-1", 5*time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, "Telemetry") || !strings.Contains(gotBody, "pump-1") {
		t.Fatalf("body did not include query/table/device: %s", gotBody)
	}
	if len(result.Rows) != 1 || result.Rows[0]["DeviceId"] != "pump-1" {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestConfigRequiresSimpleIdentifiers(t *testing.T) {
	_, err := NewClient(Config{
		Endpoint:     "https://example.test",
		Database:     "Factory",
		Table:        "Telemetry | take 1",
		TimeColumn:   "Timestamp",
		DeviceColumn: "DeviceId",
	}, fakeCredential{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
