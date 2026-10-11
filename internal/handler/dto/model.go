package dto

import (
	"context"
	"time"

	"reliquary/internal/types"
)

// ModelResponse mirrors types.Model for response bodies, with all secret
// fields (APIKey, AppSecret) removed by construction. Credential presence
// metadata lives behind the /credentials subresource, not inlined here.
//
// BaseURL is preserved for space-owned models (the frontend needs it to
// render which endpoint a custom model points at). For builtin models it is
// stripped along with every other field that could leak how a particular
// space configured the upstream provider.
type ModelResponse struct {
	ID          string             `json:"id"`
	SpaceID     uint64             `json:"space_id"`
	Name        string             `json:"name"`
	DisplayName string             `json:"display_name"`
	Type        types.ModelType    `json:"type"`
	Source      types.ModelSource  `json:"source"`
	Description string             `json:"description"`
	Parameters  ModelParametersDTO `json:"parameters"`
	IsDefault   bool               `json:"is_default"`
	IsBuiltin   bool               `json:"is_builtin"`
	Status      types.ModelStatus  `json:"status"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	// Per-field "configured?" map. Built-in model metadata requires an
	// authenticated caller. Credential values never appear in this response.
	Credentials map[string]CredentialFieldMetadata `json:"credentials,omitempty"`
}

// ModelParametersDTO carries every parameter field except APIKey and AppSecret.
// CustomHeaders is kept as structural metadata rather than a credential.
type ModelParametersDTO struct {
	BaseURL             string                    `json:"base_url"`
	InterfaceType       string                    `json:"interface_type"`
	EmbeddingParameters types.EmbeddingParameters `json:"embedding_parameters"`
	ParameterSize       string                    `json:"parameter_size"`
	Provider            string                    `json:"provider"`
	ExtraConfig         map[string]string         `json:"extra_config,omitempty"`
	CustomHeaders       map[string]string         `json:"custom_headers,omitempty"`
	SupportsVision      bool                      `json:"supports_vision"`
	ContextWindow       int                       `json:"context_window,omitempty"`
	MaxOutputTokens     int                       `json:"max_output_tokens,omitempty"`
	MaxConcurrency      int                       `json:"max_concurrency,omitempty"`
	RequestsPerMinute   int                       `json:"requests_per_minute,omitempty"`
}

// NewModelResponse converts a stored Model into its response shape.
//
// Builtin models are shared across spaces — strip BaseURL (which can leak
// the space's private endpoint) and any non-shared parameters.
func NewModelResponse(ctx context.Context, m *types.Model) *ModelResponse {
	if m == nil {
		return nil
	}
	params := ModelParametersDTO{
		BaseURL:             m.Parameters.BaseURL,
		InterfaceType:       m.Parameters.InterfaceType,
		EmbeddingParameters: m.Parameters.EmbeddingParameters,
		ParameterSize:       m.Parameters.ParameterSize,
		Provider:            m.Parameters.Provider,
		ExtraConfig:         m.Parameters.ExtraConfig,
		CustomHeaders:       m.Parameters.CustomHeaders,
		SupportsVision:      m.Parameters.SupportsVision,
		ContextWindow:       m.Parameters.ContextWindow,
		MaxOutputTokens:     m.Parameters.MaxOutputTokens,
		MaxConcurrency:      m.Parameters.MaxConcurrency,
		RequestsPerMinute:   m.Parameters.RequestsPerMinute,
	}
	canManageBuiltin := m.IsBuiltin && types.IsAuthenticatedFromContext(ctx)
	if !CanViewIntegrationSecrets(ctx) && !canManageBuiltin {
		params.ExtraConfig = nil
		params.CustomHeaders = nil
		params.BaseURL = ""
	}
	if m.IsBuiltin && !canManageBuiltin {
		// Builtin: strip everything that could reveal per-space config.
		// EmbeddingParameters and ParameterSize / Provider / InterfaceType /
		// SupportsVision / ContextWindow / MaxOutputTokens are intentionally
		// preserved (they describe the capability surface, not the configured
		// endpoint).
		params.BaseURL = ""
		params.ExtraConfig = nil
		params.CustomHeaders = nil
	}
	var creds map[string]CredentialFieldMetadata
	if !m.IsBuiltin || canManageBuiltin {
		creds = map[string]CredentialFieldMetadata{
			"api_key":    {Configured: m.Parameters.APIKey != ""},
			"app_secret": {Configured: m.Parameters.AppSecret != ""},
		}
	}
	return &ModelResponse{
		ID:          m.ID,
		SpaceID:     m.SpaceID,
		Name:        m.Name,
		DisplayName: m.DisplayName,
		Type:        m.Type,
		Source:      m.Source,
		Description: m.Description,
		Parameters:  params,
		IsDefault:   m.IsDefault,
		IsBuiltin:   m.IsBuiltin,
		Status:      m.Status,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		Credentials: creds,
	}
}

// NewModelResponses is the slice convenience wrapper.
func NewModelResponses(ctx context.Context, models []*types.Model) []*ModelResponse {
	out := make([]*ModelResponse, 0, len(models))
	for _, m := range models {
		out = append(out, NewModelResponse(ctx, m))
	}
	return out
}
