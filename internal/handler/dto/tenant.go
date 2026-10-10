package dto

import (
	"context"
	"time"

	"gorm.io/gorm"

	"reliquary/internal/types"
)

type TenantResponse struct {
	ID                 uint64                    `json:"id"`
	Name               string                    `json:"name"`
	Description        string                    `json:"description"`
	Status             string                    `json:"status"`
	Business           string                    `json:"business"`
	StorageUsed        int64                     `json:"storage_used"`
	ParserEngineConfig *types.ParserEngineConfig `json:"parser_engine_config,omitempty"`
	Credentials        *types.CredentialsConfig  `json:"credentials,omitempty"`
	RetrievalConfig    *types.RetrievalConfig    `json:"retrieval_config,omitempty"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
	DeletedAt          gorm.DeletedAt            `json:"deleted_at"`
}

// NewTenantResponse converts a stored tenant into its HTTP response shape.
func NewTenantResponse(ctx context.Context, tenant *types.Tenant) *TenantResponse {
	return NewTenantResponseWithSecrets(tenant)
}

// NewTenantResponseWithSecrets converts stored personal-space data with redacted credentials.
func NewTenantResponseWithSecrets(tenant *types.Tenant) *TenantResponse {
	if tenant == nil {
		return nil
	}
	includeSecrets := true
	resp := &TenantResponse{
		ID:              tenant.ID,
		Name:            tenant.Name,
		Description:     tenant.Description,
		Status:          tenant.Status,
		Business:        tenant.Business,
		StorageUsed:     tenant.StorageUsed,
		RetrievalConfig: tenant.RetrievalConfig,
		CreatedAt:       tenant.CreatedAt,
		UpdatedAt:       tenant.UpdatedAt,
		DeletedAt:       tenant.DeletedAt,
	}
	if includeSecrets {
		resp.ParserEngineConfig = types.ParserEngineConfigForResponse(tenant.ParserEngineConfig, true)
		resp.Credentials = types.CredentialsConfigForResponse(tenant.Credentials, true)
	}
	return resp
}

// NewTenantResponses is the slice convenience wrapper.
func NewTenantResponses(ctx context.Context, tenants []*types.Tenant) []*TenantResponse {
	out := make([]*TenantResponse, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, NewTenantResponse(ctx, t))
	}
	return out
}
