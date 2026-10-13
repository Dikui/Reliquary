package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// IngestService provides the shared external ingestion entry points for HTTP and MCP.
type IngestService interface {
	Ingest(context.Context, *types.IngestRequest) (*types.IngestResult, error)
	IngestBatch(context.Context, *types.IngestBatchRequest) (*types.IngestBatchResult, error)
	CheckURLs(context.Context, []string) ([]types.IngestCheckResult, error)
	ListIncomplete(context.Context, int, int, bool) (*types.IncompleteListResult, error)
	AcceptIncomplete(context.Context, string) (*types.IngestResult, error)
	DiscardIngest(context.Context, string, string) error
	DeleteDocument(context.Context, string) error
	UploadIngestAsset(context.Context, string, []byte) (*types.IngestAssetResult, error)
}
