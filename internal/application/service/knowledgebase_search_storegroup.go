package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reliquary/internal/application/service/retriever"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

// storeResolveBudget caps the time a single search spends resolving the
// engines for its store groups. Resolution is sequential and can rebuild a
// missing engine, so the worst case is one build timeout per distinct store;
// this bounds the total rather than the individual attempt.
const storeResolveBudget = 12 * time.Second

type storeGroup struct {
	// StoreID is the bound VectorStore UUID, or "" for the env-store group
	// (KBs with VectorStoreID = NULL). Never echo this in user-facing
	// errors; use secutils.SanitizeForLog when emitting in structured logs.
	StoreID string

	OwnerTenantID uint64

	// KBIDs are the knowledge base IDs in this group. The caller MUST have
	// authorized the request to access every ID here (the trust boundary
	// is the HTTP handler / session layer, matching the pre-existing
	// chat_pipeline pattern).
	KBIDs []string

	Engine *retriever.CompositeRetrieveEngine

	// BaseParams is the immutable per-group retrieval parameter list.
	// TopK is filled in at retrieve time via paramsWithTopK.
	BaseParams []types.RetrieveParams

	TopK int
}

func (s *knowledgeBaseService) resolveStoreGroups(
	ctx context.Context,
	primary *types.KnowledgeBase,
	kbs []*types.KnowledgeBase,
	params types.SearchParams,
	matchCount int,
) ([]*storeGroup, error) {
	type partitionKey struct {
		storeID  string
		tenantID uint64
	}
	buckets := make(map[partitionKey][]*types.KnowledgeBase)
	for _, kb := range kbs {
		sid := ""
		if kb.HasVectorStore() {
			sid = *kb.VectorStoreID
		}
		key := partitionKey{storeID: sid, tenantID: kb.TenantID}
		buckets[key] = append(buckets[key], kb)
	}

	// Resolving a group can rebuild a missing store engine, which dials a
	// backend. Those rebuilds happen one after another here, and this server
	// sets no read or write timeout, so a search across several cold stores
	// would otherwise have nothing bounding it. The rebuild itself is detached
	// from this context, so an exhausted budget still leaves the engines
	// warming and the next search finds them ready.
	resolveCtx, cancelResolve := context.WithTimeout(ctx, storeResolveBudget)
	defer cancelResolve()

	groups := make([]*storeGroup, 0, len(buckets))
	for key, groupKBs := range buckets {
		var storeIDPtr *string
		if key.storeID != "" {
			sid := key.storeID
			storeIDPtr = &sid
		}
		engine, err := retriever.CreateRetrieveEngineForKB(
			resolveCtx, s.retrieveEngine, s.ownership, key.tenantID, storeIDPtr)
		if err != nil {
			return nil, classifyFactoryError(ctx, err, key.tenantID, key.storeID)
		}
		baseParams, err := s.buildRetrievalParams(
			ctx, engine, primary, groupKBs, params, matchCount)
		if err != nil {
			return nil, fmt.Errorf("build store-group params: %w", err)
		}
		ids := make([]string, len(groupKBs))
		for i, kb := range groupKBs {
			ids[i] = kb.ID
		}
		groups = append(groups, &storeGroup{
			StoreID:       key.storeID,
			OwnerTenantID: key.tenantID,
			KBIDs:         ids,
			Engine:        engine,
			BaseParams:    baseParams,
			TopK:          matchCount,
		})
	}
	return groups, nil
}

// classifyFactoryError translates retriever sentinels into typed AppErrors
// without leaking the store UUID into the user-facing message. The UUID is
// recorded in the structured log only, sanitized via SanitizeForLog to
// defeat log-injection through CR/LF in store IDs.
func classifyFactoryError(
	ctx context.Context, err error, tenantID uint64, storeID string,
) error {
	logger.WarnWithFields(ctx, logger.Fields{
		"tenant_id": tenantID,
		"store_id":  secutils.SanitizeForLog(storeID),
		"reason":    "resolve store engine",
	}, err.Error())
	switch {
	case errors.Is(err, retriever.ErrVectorStoreForbidden):
		return apperrors.NewVectorStoreBindingInvalidError(
			"vector store bound to the knowledge base is not available")
	case errors.Is(err, retriever.ErrVectorStoreUnavailable):
		return apperrors.NewVectorStoreUnavailableError(
			"vector store is currently unavailable")
	case errors.Is(err, retriever.ErrVectorStoreNotFound):
		return apperrors.NewVectorStoreUnavailableError(
			"vector store is currently unavailable")
	case errors.Is(err, context.DeadlineExceeded):
		// Resolving the store ran out of time, which can happen while its
		// engine is being rebuilt. The binding is fine and a retry may work,
		// so report it as unavailable rather than letting it fall through as
		// an internal error.
		return apperrors.NewVectorStoreUnavailableError(
			"vector store is currently unavailable")
	case errors.Is(err, retriever.ErrTenantInfoMissing):
		return apperrors.NewVectorStoreBindingInvalidError(
			"tenant information missing in context")
	default:
		return err
	}
}

func (s *knowledgeBaseService) authorizeKBAccess(ctx context.Context, kbs []*types.KnowledgeBase, tenantID uint64) error {
	for _, kb := range kbs {
		if kb == nil || kb.TenantID != tenantID {
			return apperrors.NewForbiddenError("knowledge base is outside the team")
		}
	}
	return nil
}

// validateSameEmbeddingModel rejects multi-KB searches that span more than
// one resolved embedding-model identity key. Single-KB calls no-op.
//
// Wiki-only / graph-only KBs (empty resolved key) are tolerated: if every
// KB lacks an embedding model, validation passes and HybridSearch returns
// an empty result set via the allBaseParamsEmpty fast path.
//
// Log fields are sanitized via secutils.SanitizeForLog because resolved
// keys are derived from model.Parameters.BaseURL, which is tenant-
// configured and can contain CR/LF or other control characters.
func (s *knowledgeBaseService) validateSameEmbeddingModel(
	ctx context.Context,
	kbs []*types.KnowledgeBase,
) error {
	if len(kbs) <= 1 {
		return nil
	}
	keys := s.ResolveEmbeddingModelKeys(ctx, kbs)
	var seen string
	for _, kb := range kbs {
		k, ok := keys[kb.ID]
		if !ok || k == "" {
			// Wiki-only / graph-only carve-out: KB has no embedding model.
			continue
		}
		if seen == "" {
			seen = k
			continue
		}
		if k != seen {
			logger.WarnWithFields(ctx, logger.Fields{
				"primary_key": secutils.SanitizeForLog(seen),
				"diverging":   secutils.SanitizeForLog(k),
				"kb_id":       kb.ID,
				"kb_count":    len(kbs),
			}, "multi-KB search rejected: embedding models differ")
			return apperrors.NewBadRequestError(
				"selected knowledge bases use different embedding models; " +
					"multi-KB search requires every knowledge base to share a single embedding model")
		}
	}
	return nil
}
