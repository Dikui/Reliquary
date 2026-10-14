package rerank

import (
	"context"
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
