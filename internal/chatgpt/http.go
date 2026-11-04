package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"reliquary/internal/types"
)

const maxOAuthResponseBytes = 2 * 1024 * 1024

// EndpointError preserves machine diagnostics without retaining token bodies.
type EndpointError struct {
	Status    int
	Code      string
	RequestID string
}

func (e *EndpointError) Error() string {
	return fmt.Sprintf("chatgpt: upstream status %d (%s), request ID %s", e.Status, e.Code, e.RequestID)
}

type tokenResponse struct {
	AccessToken       string          `json:"access_token"`
	RefreshToken      string          `json:"refresh_token"`
	IDToken           string          `json:"id_token"`
	TokenType         string          `json:"token_type"`
	ExpiresIn         int64           `json:"expires_in"`
	Scope             string          `json:"scope"`
	EarliestRefreshAt json.RawMessage `json:"earliest_refresh_at"`
}

func validTokenSet(tokens *tokenResponse) error {
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.ExpiresIn <= 0 || !strings.EqualFold(tokens.TokenType, "Bearer") {
		return fmt.Errorf("chatgpt: incomplete bearer token response")
	}
	return nil
}

func (m *Manager) request(ctx context.Context, method, endpoint string, form url.Values, token string) ([]byte, int, string, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, "", fmt.Errorf("chatgpt: invalid upstream endpoint")
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := m.client.Do(req)
	if err != nil {
		return nil, 0, "", &types.ChatGPTError{Kind: "transport", Message: "ChatGPT authorization request could not be sent", Cause: err}
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, maxOAuthResponseBytes+1))
	if err != nil || len(b) > maxOAuthResponseBytes {
		return nil, response.StatusCode, response.Header.Get("X-Request-ID"), fmt.Errorf("chatgpt: unreadable upstream response")
	}
	return b, response.StatusCode, response.Header.Get("X-Request-ID"), nil
}

func endpointError(body []byte, status int, requestID string) *EndpointError {
	var parsed struct {
		Error json.RawMessage `json:"error"`
	}
	code := "upstream_error"
	if json.Unmarshal(body, &parsed) == nil {
		var text string
		if json.Unmarshal(parsed.Error, &text) == nil && text != "" {
			code = text
		} else {
			var obj struct {
				Code string `json:"code"`
			}
			if json.Unmarshal(parsed.Error, &obj) == nil && obj.Code != "" {
				code = obj.Code
			}
		}
	}
	return &EndpointError{Status: status, Code: code, RequestID: requestID}
}

func (m *Manager) exchange(ctx context.Context, form url.Values) (*tokenResponse, error) {
	body, status, requestID, err := m.request(ctx, http.MethodPost, m.endpoints.Token, form, "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, endpointError(body, status, requestID)
	}
	var tokens tokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("chatgpt: invalid token response")
	}
	return &tokens, nil
}

func parseEarliestRefresh(raw json.RawMessage) (*time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var seconds int64
	if json.Unmarshal(raw, &seconds) == nil {
		if seconds <= 0 {
			return nil, nil
		}
		value := time.Unix(seconds, 0).UTC()
		return &value, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil, fmt.Errorf("chatgpt: invalid earliest_refresh_at")
	}
	if text == "" {
		return nil, nil
	}
	if seconds, err := strconv.ParseInt(text, 10, 64); err == nil {
		value := time.Unix(seconds, 0).UTC()
		return &value, nil
	}
	value, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return nil, fmt.Errorf("chatgpt: invalid earliest_refresh_at")
	}
	value = value.UTC()
	return &value, nil
}

type discoveryDocument struct {
	Issuer             string `json:"issuer"`
	JWKSURI            string `json:"jwks_uri"`
	RevocationEndpoint string `json:"revocation_endpoint"`
}

func sameIssuerOrigin(endpoint, issuer string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Fragment != "" {
		return false
	}
	base, err := url.Parse(issuer)
	return err == nil && u.Scheme == base.Scheme && u.Host == base.Host && u.Scheme != "" && u.Host != ""
}

func (m *Manager) discovery(ctx context.Context) (*discoveryDocument, error) {
	body, status, requestID, err := m.request(ctx, http.MethodGet, m.endpoints.Discovery, nil, "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, endpointError(body, status, requestID)
	}
	var doc discoveryDocument
	if json.Unmarshal(body, &doc) != nil || doc.Issuer != m.endpoints.Issuer || !sameIssuerOrigin(doc.JWKSURI, doc.Issuer) {
		return nil, fmt.Errorf("chatgpt: invalid OpenID discovery document")
	}
	if doc.RevocationEndpoint != "" && !sameIssuerOrigin(doc.RevocationEndpoint, doc.Issuer) {
		return nil, fmt.Errorf("chatgpt: invalid revocation endpoint")
	}
	return &doc, nil
}

func (m *Manager) Models(ctx context.Context, accountID string) ([]Model, error) {
	token, err := m.AccessToken(ctx, accountID)
	if err != nil {
		return nil, err
	}
	body, status, requestID, err := m.request(ctx, http.MethodGet, m.endpoints.Models, nil, token)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, endpointError(body, status, requestID)
	}
	var result struct {
		Models []Model `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("chatgpt: invalid model catalog")
	}
	models := make([]Model, 0, len(result.Models))
	for _, model := range result.Models {
		if model.Visibility == "list" && model.Slug != "" {
			models = append(models, model)
		}
	}
	return models, nil
}
