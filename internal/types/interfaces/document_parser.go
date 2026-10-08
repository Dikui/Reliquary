package interfaces

import (
	"context"

	"reliquary/internal/types"
)

// DocReader is the core interface for reading documents into markdown.
type DocReader interface {
	Read(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error)
}
