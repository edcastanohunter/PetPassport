package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/opensearch-project/opensearch-go/v2"
	"github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

// OpenSearchConfig contiene las opciones de conexión con OpenSearch.
type OpenSearchConfig struct {
	Addresses []string
	Username  string
	Password  string
	Insecure  bool
	Transport http.RoundTripper // opcional, para pruebas o mocks
}

// Client define la interfaz que encapsula el cliente de OpenSearch.
// Solo este paquete (internal/search/app) interactúa con opensearch-go.
type Client interface {
	Ping(ctx context.Context) error
	IndexExists(ctx context.Context, index string) (bool, error)
	CreateIndex(ctx context.Context, index string, body []byte) error
	DeleteIndex(ctx context.Context, index string) error
	GetAliases(ctx context.Context) (map[string][]string, error)
	UpdateAliases(ctx context.Context, actions []AliasAction) error
	Bulk(ctx context.Context, items []BulkOperation) (*BulkResponse, error)
	Search(ctx context.Context, index string, query map[string]any) (*SearchRawResult, error)
	UpdateByQuery(ctx context.Context, index string, query map[string]any, script map[string]any) (int64, error)
	Delete(ctx context.Context, index, id string) error
	CatIndices(ctx context.Context) ([]IndexStat, error)
}

// AliasAction representa una acción atómica sobre aliases de OpenSearch.
type AliasAction struct {
	Type  string // "add" o "remove"
	Index string
	Alias string
}

// BulkOperation representa una operación en un lote _bulk.
type BulkOperation struct {
	Action      string // "index", "create", "delete", "update"
	Index       string
	ID          string
	Version     int64
	VersionType string // "external_gte"
	Document    any
}

// BulkResponse representa el resultado de una llamada _bulk.
type BulkResponse struct {
	Took   int64 `json:"took"`
	Errors bool  `json:"errors"`
	Items  []map[string]struct {
		Index   string `json:"_index"`
		ID      string `json:"_id"`
		Version int64  `json:"_version"`
		Result  string `json:"result"`
		Status  int    `json:"status"`
		Error   *struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error,omitempty"`
	} `json:"items"`
}

// SearchRawResult representa la respuesta cruda de OpenSearch.
type SearchRawResult struct {
	Took     int64 `json:"took"`
	TimedOut bool  `json:"timed_out"`
	Hits     struct {
		Total struct {
			Value    int64  `json:"value"`
			Relation string `json:"relation"`
		} `json:"total"`
		MaxScore *float64 `json:"max_score"`
		Hits     []struct {
			Index     string              `json:"_index"`
			ID        string              `json:"_id"`
			Score     *float64            `json:"_score"`
			Source    json.RawMessage     `json:"_source"`
			Highlight map[string][]string `json:"highlight,omitempty"`
			Sort      []any               `json:"sort,omitempty"`
		} `json:"hits"`
	} `json:"hits"`
	Aggregations json.RawMessage `json:"aggregations,omitempty"`
}

// IndexStat contiene métricas básicas de un índice físico.
type IndexStat struct {
	Index         string `json:"index"`
	Health        string `json:"health"`
	Status        string `json:"status"`
	DocsCount     string `json:"docs.count"`
	StoreSize     string `json:"store.size"`
	PrimaryShards string `json:"pri"`
}

type osClient struct {
	client *opensearch.Client
}

// NewClient construye un nuevo cliente de OpenSearch conectado a la dirección configurada.
func NewClient(cfg OpenSearchConfig) (Client, error) {
	if len(cfg.Addresses) == 0 {
		cfg.Addresses = []string{"http://localhost:9200"}
	}

	transport := cfg.Transport
	if transport == nil {
		transport = &http.Transport{
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.Insecure, // #nosec G402 -- configurable para desarrollo local
			},
		}
	}

	osCfg := opensearch.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
		Transport: transport,
	}

	c, err := opensearch.NewClient(osCfg)
	if err != nil {
		return nil, fmt.Errorf("creating opensearch client: %w", err)
	}

	return &osClient{client: c}, nil
}

func (c *osClient) Ping(ctx context.Context) error {
	req := opensearchapi.PingRequest{}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		return fmt.Errorf("ping error: status %d", res.StatusCode)
	}
	return nil
}

func (c *osClient) IndexExists(ctx context.Context, index string) (bool, error) {
	req := opensearchapi.IndicesExistsRequest{
		Index: []string{index},
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return false, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusOK {
		return true, nil
	}
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("indices.exists error: status %d", res.StatusCode)
}

func (c *osClient) CreateIndex(ctx context.Context, index string, body []byte) error {
	req := opensearchapi.IndicesCreateRequest{
		Index: index,
		Body:  bytes.NewReader(body),
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("create index %s error: status %d: %s", index, res.StatusCode, string(b))
	}
	return nil
}

func (c *osClient) DeleteIndex(ctx context.Context, index string) error {
	req := opensearchapi.IndicesDeleteRequest{
		Index: []string{index},
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("delete index %s error: status %d: %s", index, res.StatusCode, string(b))
	}
	return nil
}

func (c *osClient) GetAliases(ctx context.Context) (map[string][]string, error) {
	req := opensearchapi.IndicesGetAliasRequest{}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("get alias error: status %d: %s", res.StatusCode, string(b))
	}

	var raw map[string]struct {
		Aliases map[string]any `json:"aliases"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make(map[string][]string) // alias -> []index
	for indexName, item := range raw {
		for aliasName := range item.Aliases {
			out[aliasName] = append(out[aliasName], indexName)
		}
	}
	return out, nil
}

func (c *osClient) UpdateAliases(ctx context.Context, actions []AliasAction) error {
	var actionsPayload []map[string]any
	for _, a := range actions {
		switch a.Type {
		case "add":
			actionsPayload = append(actionsPayload, map[string]any{
				"add": map[string]any{"index": a.Index, "alias": a.Alias},
			})
		case "remove":
			actionsPayload = append(actionsPayload, map[string]any{
				"remove": map[string]any{"index": a.Index, "alias": a.Alias},
			})
		}
	}

	body, err := json.Marshal(map[string]any{"actions": actionsPayload})
	if err != nil {
		return err
	}

	req := opensearchapi.IndicesUpdateAliasesRequest{
		Body: bytes.NewReader(body),
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("update aliases error: status %d: %s", res.StatusCode, string(b))
	}
	return nil
}

func (c *osClient) Bulk(ctx context.Context, items []BulkOperation) (*BulkResponse, error) {
	if len(items) == 0 {
		return &BulkResponse{}, nil
	}

	var buf bytes.Buffer
	for _, item := range items {
		meta := map[string]any{
			"_index": item.Index,
			"_id":    item.ID,
		}
		if item.Version > 0 {
			meta["version"] = item.Version
			if item.VersionType != "" {
				meta["version_type"] = item.VersionType
			}
		}

		action := item.Action
		if action == "" {
			action = "index"
		}
		header := map[string]any{action: meta}
		headerBytes, err := json.Marshal(header)
		if err != nil {
			return nil, err
		}
		buf.Write(headerBytes)
		buf.WriteByte('\n')

		if action != "delete" && item.Document != nil {
			docBytes, err := json.Marshal(item.Document)
			if err != nil {
				return nil, err
			}
			buf.Write(docBytes)
			buf.WriteByte('\n')
		}
	}

	req := opensearchapi.BulkRequest{
		Body: bytes.NewReader(buf.Bytes()),
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("bulk error: status %d: %s", res.StatusCode, string(b))
	}

	var resp BulkResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *osClient) Search(ctx context.Context, index string, query map[string]any) (*SearchRawResult, error) {
	queryBytes, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req := opensearchapi.SearchRequest{
		Index: []string{index},
		Body:  bytes.NewReader(queryBytes),
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("search error on index %s: status %d: %s", index, res.StatusCode, string(b))
	}

	var raw SearchRawResult
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

func (c *osClient) UpdateByQuery(ctx context.Context, index string, query map[string]any, script map[string]any) (int64, error) {
	payload := map[string]any{
		"query": query,
	}
	if script != nil {
		payload["script"] = script
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req := opensearchapi.UpdateByQueryRequest{
		Index: []string{index},
		Body:  bytes.NewReader(bodyBytes),
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return 0, fmt.Errorf("update_by_query error: status %d: %s", res.StatusCode, string(b))
	}

	var result struct {
		Updated int64 `json:"updated"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Updated, nil
}

func (c *osClient) Delete(ctx context.Context, index, id string) error {
	req := opensearchapi.DeleteRequest{
		Index:      index,
		DocumentID: id,
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("delete doc %s/%s error: status %d: %s", index, id, res.StatusCode, string(b))
	}
	return nil
}

func (c *osClient) CatIndices(ctx context.Context) ([]IndexStat, error) {
	req := opensearchapi.CatIndicesRequest{
		Format: "json",
	}
	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("cat indices error: status %d: %s", res.StatusCode, string(b))
	}

	var stats []IndexStat
	if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
		// Cat indices might return text if format isn't supported
		return nil, nil
	}
	// Filtrar índices internos que empiezan con '.'
	var userIndices []IndexStat
	for _, s := range stats {
		if !strings.HasPrefix(s.Index, ".") {
			userIndices = append(userIndices, s)
		}
	}
	return userIndices, nil
}
