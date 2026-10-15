package dto

import (
	"context"
	"time"

	"gorm.io/gorm"

	"reliquary/internal/types"
)

type SpaceResponse struct {
	EmbeddingModelID string                   `json:"embedding_model_id"`
	ID               uint64                   `json:"id"`
	SpaceID          uint64                   `json:"space_id"`
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	Status           string                   `json:"status"`
	Business         string                   `json:"business"`
	StorageUsed      int64                    `json:"storage_used"`
	Credentials      *types.CredentialsConfig `json:"credentials,omitempty"`
	RetrievalConfig  *types.RetrievalConfig   `json:"retrieval_config,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	DeletedAt        gorm.DeletedAt           `json:"deleted_at"`
}

// NewSpaceResponse converts a stored space into its HTTP response shape.
func NewSpaceResponse(ctx context.Context, space *types.Space) *SpaceResponse {
	return NewSpaceResponseWithSecrets(space)
}

// NewSpaceResponseWithSecrets converts stored personal-space data with redacted credentials.
func NewSpaceResponseWithSecrets(space *types.Space) *SpaceResponse {
	if space == nil {
		return nil
	}
	includeSecrets := true
	resp := &SpaceResponse{
		ID:               space.ID,
		EmbeddingModelID: space.EmbeddingModelID,
		SpaceID:          space.ID,
		Name:             space.Name,
		Description:      space.Description,
		Status:           space.Status,
		Business:         space.Business,
		StorageUsed:      space.StorageUsed,
		RetrievalConfig:  space.RetrievalConfig,
		CreatedAt:        space.CreatedAt,
		UpdatedAt:        space.UpdatedAt,
		DeletedAt:        space.DeletedAt,
	}
	if includeSecrets {
		resp.Credentials = types.CredentialsConfigForResponse(space.Credentials, true)
	}
	return resp
}

// NewSpaceResponses is the slice convenience wrapper.
func NewSpaceResponses(ctx context.Context, spaces []*types.Space) []*SpaceResponse {
	out := make([]*SpaceResponse, 0, len(spaces))
	for _, t := range spaces {
		out = append(out, NewSpaceResponse(ctx, t))
	}
	return out
}
