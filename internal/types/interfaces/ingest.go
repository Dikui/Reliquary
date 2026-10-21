package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// IngestService provides the shared external ingestion entry points for HTTP and MCP.
type IngestService interface {
	Ingest(context.Context, *types.IngestRequest) (*types.IngestResult, error)
	IngestBatch(context.Context, *types.IngestBatchRequest) (*types.IngestBatchResult, error)
	CheckURLs(context.Context, []string, ...string) ([]types.IngestCheckResult, error)
	DiscardIngest(context.Context, string, string) error
	DeleteDocument(context.Context, string) error
	UpdateEntryContent(context.Context, string, *types.EntryContentUpdate) (*types.IngestResult, error)
	UploadIngestAsset(context.Context, string, []byte) (*types.IngestAssetResult, error)
}
