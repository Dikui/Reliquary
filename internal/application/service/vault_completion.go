package service

import (
	"context"
	"encoding/json"
	"io"

	"reliquary/internal/types"
)

func ingestCompletionPending(k *types.Entry) bool {
	if k == nil {
		return false
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(k.Metadata, &metadata) != nil {
		return false
	}
	var pending bool
	_ = json.Unmarshal(metadata["completion_pending"], &pending)
	return pending
}

func (v *VaultService) prepareArticleMaterials(ctx context.Context, k *types.Entry, articlePath, body string, read func(context.Context, string) (io.ReadCloser, error)) (string, error) {
	return v.copyArticleImages(ctx, k, articlePath, body, read)
}
