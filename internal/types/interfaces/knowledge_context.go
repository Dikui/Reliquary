package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// KnowledgeContextReader lets the chat pipeline consume structured evidence
// without importing the application service package that owns the pipeline.
type KnowledgeContextReader interface {
	Query(context.Context, string, types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error)
}
