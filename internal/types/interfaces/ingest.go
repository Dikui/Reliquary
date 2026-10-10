package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// IngestService provides the shared external collection entry point for HTTP and MCP.
type IngestService interface {
	Ingest(context.Context, *types.IngestRequest) (*types.IngestResult, error)
}
