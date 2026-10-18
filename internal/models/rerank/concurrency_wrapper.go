package rerank

import (
	"context"
	"fmt"

	"reliquary/internal/models/limiter"
	"reliquary/internal/types"
)

type governedReranker struct {
	accounting               *types.ModelCallAccounting
	inner                    Reranker
	limit, requestsPerMinute int
}

func (w *governedReranker) GetModelID() string   { return w.inner.GetModelID() }
func (w *governedReranker) GetModelName() string { return w.inner.GetModelName() }
func (w *governedReranker) Rerank(ctx context.Context, query string, documents []string) ([]RankResult, error) {
	ctx = types.WithModelTier(ctx, types.ModelTierRerank)
	if err := limiter.WaitRate(ctx, w.GetModelID(), w.requestsPerMinute); err != nil {
		return nil, err
	}
	release := limiter.GateNamedN(ctx, w.GetModelID(), w.GetModelName(), w.limit)
	defer release()
	ctx = types.WithModelCallAccounting(ctx, w.accounting)
	types.RecordModelCall(ctx, w.GetModelID())
	return w.inner.Rerank(ctx, query, documents)
}

func (w *governedReranker) SupportsImages() bool {
	r, ok := w.inner.(MultimodalReranker)
	return ok && r.SupportsImages()
}

func (w *governedReranker) RerankMultimodal(ctx context.Context, query string, documents []RerankDocument) ([]RankResult, error) {
	r, ok := w.inner.(MultimodalReranker)
	if !ok || !r.SupportsImages() {
		return nil, fmt.Errorf("rerank model %q does not support images", w.GetModelName())
	}
	ctx = types.WithModelTier(ctx, types.ModelTierRerank)
	if err := limiter.WaitRate(ctx, w.GetModelID(), w.requestsPerMinute); err != nil {
		return nil, err
	}
	release := limiter.GateNamedN(ctx, w.GetModelID(), w.GetModelName(), w.limit)
	defer release()
	ctx = types.WithModelCallAccounting(ctx, w.accounting)
	types.RecordModelCall(ctx, w.GetModelID())
	return r.RerankMultimodal(ctx, query, documents)
}
