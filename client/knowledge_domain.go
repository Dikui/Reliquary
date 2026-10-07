package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

type DomainField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Aliases     []string `json:"aliases"`
	ValueType   string   `json:"value_type"`
	UnitFamily  string   `json:"unit_family,omitempty"`
	EntityTypes []string `json:"entity_types"`
}

type DomainRelation struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Aliases      []string `json:"aliases"`
	SubjectTypes []string `json:"subject_types"`
	ObjectTypes  []string `json:"object_types"`
}

type DomainUnit struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Factor  float64  `json:"factor"`
}

type DomainUnitFamily struct {
	Key           string       `json:"key"`
	CanonicalUnit string       `json:"canonical_unit"`
	Units         []DomainUnit `json:"units"`
}

// KnowledgeDomain defines the vocabulary used by its knowledge bases.
type KnowledgeDomain struct {
	ID              string             `json:"id"`
	TenantID        uint64             `json:"tenant_id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	EntityTypes     []string           `json:"entity_types"`
	Fields          []DomainField      `json:"fields"`
	Relations       []DomainRelation   `json:"relations"`
	UnitFamilies    []DomainUnitFamily `json:"unit_families"`
	ExtractionHints string             `json:"extraction_hints"`
	IsBuiltin       bool               `json:"is_builtin"`
	CreatedBy       string             `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type DomainOpenField struct {
	Name         string            `json:"name"`
	Count        int64             `json:"count"`
	ExampleFacts []json.RawMessage `json:"example_facts"`
}

func (c *Client) ListDomainOpenFields(ctx context.Context, id string) ([]DomainOpenField, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/knowledge-domains/"+url.PathEscape(id)+"/open-fields", nil, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []DomainOpenField `json:"data"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *Client) MapDomainOpenField(ctx context.Context, id, openField, fieldKey string) (*KnowledgeDomain, error) {
	return c.knowledgeDomainRequest(ctx, http.MethodPost, "/api/v1/knowledge-domains/"+url.PathEscape(id)+"/field-mappings", map[string]string{"open_field": openField, "field_key": fieldKey})
}

func (c *Client) AddDomainField(ctx context.Context, id string, field DomainField) (*KnowledgeDomain, error) {
	return c.knowledgeDomainRequest(ctx, http.MethodPost, "/api/v1/knowledge-domains/"+url.PathEscape(id)+"/fields", field)
}

func (c *Client) ListKnowledgeDomains(ctx context.Context) ([]KnowledgeDomain, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/knowledge-domains", nil, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []KnowledgeDomain `json:"data"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (c *Client) GetKnowledgeDomain(ctx context.Context, id string) (*KnowledgeDomain, error) {
	return c.knowledgeDomainRequest(ctx, http.MethodGet, "/api/v1/knowledge-domains/"+url.PathEscape(id), nil)
}

func (c *Client) CreateKnowledgeDomain(ctx context.Context, domain *KnowledgeDomain) (*KnowledgeDomain, error) {
	return c.knowledgeDomainRequest(ctx, http.MethodPost, "/api/v1/knowledge-domains", domain)
}

func (c *Client) UpdateKnowledgeDomain(ctx context.Context, id string, domain *KnowledgeDomain) (*KnowledgeDomain, error) {
	return c.knowledgeDomainRequest(ctx, http.MethodPut, "/api/v1/knowledge-domains/"+url.PathEscape(id), domain)
}

func (c *Client) DeleteKnowledgeDomain(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/api/v1/knowledge-domains/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return err
	}
	var result struct {
		Success bool `json:"success"`
	}
	return parseResponse(resp, &result)
}

func (c *Client) knowledgeDomainRequest(ctx context.Context, method, path string, body any) (*KnowledgeDomain, error) {
	resp, err := c.doRequest(ctx, method, path, body, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data KnowledgeDomain `json:"data"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}
