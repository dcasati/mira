package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

const DefaultTokenScope = "https://kusto.kusto.windows.net/.default"

var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Config struct {
	Endpoint        string
	Database        string
	Table           string
	TimeColumn      string
	DeviceColumn    string
	DefaultLookback time.Duration
	MaxRows         int
	TokenScope      string
}

type Client struct {
	cfg    Config
	cred   azcore.TokenCredential
	client *http.Client
}

type QueryResult struct {
	Query   string           `json:"query"`
	Rows    []map[string]any `json:"rows"`
	Columns []string         `json:"columns"`
	Meta    map[string]any   `json:"meta,omitempty"`
}

type kustoRequest struct {
	Database string `json:"db"`
	Query    string `json:"csl"`
}

type kustoResponse struct {
	Tables []struct {
		TableName string `json:"TableName"`
		Columns   []struct {
			ColumnName string `json:"ColumnName"`
			DataType   string `json:"DataType"`
		} `json:"Columns"`
		Rows [][]any `json:"Rows"`
	} `json:"Tables"`
}

func NewClient(cfg Config, cred azcore.TokenCredential) (*Client, error) {
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.Database = strings.TrimSpace(cfg.Database)
	cfg.Table = strings.TrimSpace(cfg.Table)
	if cfg.TimeColumn == "" {
		cfg.TimeColumn = "Timestamp"
	}
	if cfg.DeviceColumn == "" {
		cfg.DeviceColumn = "DeviceId"
	}
	if cfg.DefaultLookback <= 0 {
		cfg.DefaultLookback = 15 * time.Minute
	}
	if cfg.MaxRows <= 0 {
		cfg.MaxRows = 20
	}
	if cfg.TokenScope == "" {
		cfg.TokenScope = DefaultTokenScope
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, cred: cred, client: http.DefaultClient}, nil
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.Endpoint) != "" || strings.TrimSpace(c.Database) != "" || strings.TrimSpace(c.Table) != ""
}

func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	var errs []error
	if c.Endpoint == "" {
		errs = append(errs, errors.New("FABRIC_KQL_ENDPOINT is required when telemetry is enabled"))
	}
	if c.Database == "" {
		errs = append(errs, errors.New("FABRIC_KQL_DATABASE is required when telemetry is enabled"))
	}
	for name, value := range map[string]string{
		"FABRIC_KQL_TABLE":             c.Table,
		"MIRA_TELEMETRY_TIME_COLUMN":   c.TimeColumn,
		"MIRA_TELEMETRY_DEVICE_COLUMN": c.DeviceColumn,
	} {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s is required when telemetry is enabled", name))
			continue
		}
		if !identifierRE.MatchString(value) {
			errs = append(errs, fmt.Errorf("%s must be a simple KQL identifier", name))
		}
	}
	if _, err := url.ParseRequestURI(c.Endpoint); err != nil && c.Endpoint != "" {
		errs = append(errs, fmt.Errorf("FABRIC_KQL_ENDPOINT is invalid: %w", err))
	}
	return errors.Join(errs...)
}

func (c *Client) Recent(ctx context.Context, deviceID string, lookback time.Duration, limit int) (QueryResult, error) {
	if lookback <= 0 {
		lookback = c.cfg.DefaultLookback
	}
	if limit <= 0 || limit > c.cfg.MaxRows {
		limit = c.cfg.MaxRows
	}
	query := fmt.Sprintf("%s\n| where %s >= ago(%dm)", c.cfg.Table, c.cfg.TimeColumn, int(lookback.Minutes()))
	if strings.TrimSpace(deviceID) != "" {
		query += fmt.Sprintf("\n| where tostring(%s) == %s", c.cfg.DeviceColumn, stringLiteral(deviceID))
	}
	query += fmt.Sprintf("\n| order by %s desc\n| take %d", c.cfg.TimeColumn, limit)
	return c.query(ctx, query)
}

func (c *Client) Summary(ctx context.Context, deviceID, metric string, lookback time.Duration) (QueryResult, error) {
	if !identifierRE.MatchString(metric) {
		return QueryResult{}, fmt.Errorf("metric must be a simple KQL identifier")
	}
	if lookback <= 0 {
		lookback = c.cfg.DefaultLookback
	}
	query := fmt.Sprintf("%s\n| where %s >= ago(%dm)", c.cfg.Table, c.cfg.TimeColumn, int(lookback.Minutes()))
	if strings.TrimSpace(deviceID) != "" {
		query += fmt.Sprintf("\n| where tostring(%s) == %s", c.cfg.DeviceColumn, stringLiteral(deviceID))
	}
	query += fmt.Sprintf("\n| summarize count=count(), avg=avg(todouble(%s)), min=min(todouble(%s)), max=max(todouble(%s)), latest_time=max(%s) by device=tostring(%s)", metric, metric, metric, c.cfg.TimeColumn, c.cfg.DeviceColumn)
	return c.query(ctx, query)
}

func (c *Client) query(ctx context.Context, query string) (QueryResult, error) {
	endpoint := c.cfg.Endpoint
	if !strings.HasSuffix(endpoint, "/v2/rest/query") {
		endpoint += "/v2/rest/query"
	}
	token, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{c.cfg.TokenScope}})
	if err != nil {
		return QueryResult{}, err
	}
	body, err := json.Marshal(kustoRequest{Database: c.cfg.Database, Query: query})
	if err != nil {
		return QueryResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return QueryResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
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
		return QueryResult{}, fmt.Errorf("KQL query failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var parsed kustoResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return QueryResult{}, err
	}
	if len(parsed.Tables) == 0 {
		return QueryResult{Query: query, Rows: []map[string]any{}, Columns: []string{}}, nil
	}
	table := parsed.Tables[0]
	columns := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		columns[i] = col.ColumnName
	}
	rows := make([]map[string]any, 0, len(table.Rows))
	for _, raw := range table.Rows {
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			if i < len(raw) {
				row[col] = raw[i]
			}
		}
		rows = append(rows, row)
	}
	return QueryResult{
		Query:   query,
		Rows:    rows,
		Columns: columns,
		Meta: map[string]any{
			"endpoint": c.cfg.Endpoint,
			"database": c.cfg.Database,
			"table":    table.TableName,
		},
	}, nil
}

func stringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}
