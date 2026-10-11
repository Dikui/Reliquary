package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/types"
)

func TestChatGPTSynchronousContentTypeCorrectionPreservesArticleDuringPause(t *testing.T) {
	for _, stage := range []string{"article_processing", "entity_resolution"} {
		for _, code := range []string{"subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable"} {
			t.Run(stage+"/"+code, func(t *testing.T) {
				original := localCorrectionSource(t, nil)
				var correcting, fail atomic.Bool
				h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
					if fail.Load() && request.stage() == stage {
						return "", code
					}
					if request.stage() == "article_processing" {
						if correcting.Load() {
							return articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Correction quota entity", EntityType: "company"}}, []string{}), ""
						}
						return articleProcessingOutput(t, "tutorial", nil, []string{}), ""
					}
					return chatGPTGenerationSuccess(t, request)
				})
				vault, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir(), HeaderDebounce: 35 * time.Millisecond}})
				require.NoError(t, err)
				h.service.SetVault(vault)
				t.Cleanup(func() { require.NoError(t, vault.Close()) })
				article := uploadedCorrectionSummary(t, h.articleProcessingHarness, original.URL)
				require.Equal(t, types.ProcessingModeSummaryOnly, article.ProcessingMode)
				require.NoError(t, h.db.Model(article).Updates(map[string]any{
					"processing_review_status": types.ProcessingReviewPending, "processing_review_content_type": "event", "processing_review_reason": "Fixture review must remain pending",
				}).Error)
				article = h.article(t, article.ID)
				beforeFile, _, _ := readVaultArticle(t, vault, article)
				beforeChunks, beforeBodyVectors, beforeArticleVectors := correctionIndexCounts(t, h.articleProcessingHarness, article.ID)
				beforeFiles := len(h.service.fileSvc.(*articleProcessingFiles).saved)
				correcting.Store(true)
				fail.Store(true)
				var result *types.Knowledge
				if code == "subscription_sharing_usage_unavailable" {
					result, err = h.service.ResolveProcessingReview(h.ctx, article.ID, "correct", "research", false)
				} else {
					result, err = h.service.CorrectContentType(h.ctx, article.ID, "research", false)
				}
				var deferred *types.ChatGPTDeferredError
				require.ErrorAs(t, err, &deferred, "preserve the retryable pause from both generation stages")
				require.Equal(t, code, deferred.Reason)
				require.True(t, types.IsChatGPTQuotaError(err))
				require.Equal(t, article.ID, result.ID)
				after := h.article(t, article.ID)
				require.Equal(t, article.ContentType, after.ContentType)
				require.Equal(t, article.ContentTypeCorrected, after.ContentTypeCorrected)
				require.Equal(t, article.ProcessingMode, after.ProcessingMode)
				require.Equal(t, article.ParseStatus, after.ParseStatus)
				require.Equal(t, article.ErrorMessage, after.ErrorMessage)
				require.Equal(t, article.StructuredSummary, after.StructuredSummary)
				require.Equal(t, article.Metadata, after.Metadata)
				require.Equal(t, article.ProcessingReviewStatus, after.ProcessingReviewStatus)
				require.Equal(t, article.ProcessingReviewContentType, after.ProcessingReviewContentType)
				require.Equal(t, article.ProcessingReviewReason, after.ProcessingReviewReason)
				require.Equal(t, article.UpdatedAt, after.UpdatedAt)
				afterFile, _, _ := readVaultArticle(t, vault, after)
				require.Equal(t, beforeFile, afterFile)
				chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h.articleProcessingHarness, article.ID)
				require.Equal(t, beforeChunks, chunks)
				require.Equal(t, beforeBodyVectors, bodyVectors)
				require.Equal(t, beforeArticleVectors, articleVectors)
				require.Len(t, h.service.fileSvc.(*articleProcessingFiles).saved, beforeFiles, "temporary refetched files are removed")
				var logs, entityLinks int64
				require.NoError(t, h.db.Model(&types.OperationLog{}).Where("object_id=? AND operation_type IN ?", article.ID, []string{"correct_content_type", "processing_review_correct"}).Count(&logs).Error)
				require.NoError(t, h.db.Model(&types.KnowledgeEntity{}).Where("knowledge_id=?", article.ID).Count(&entityLinks).Error)
				require.Zero(t, logs)
				require.Zero(t, entityLinks)
				policy, err := h.policy.Get(context.Background())
				require.NoError(t, err)
				require.True(t, policy.Paused)
				require.Equal(t, code, policy.Reason)
				fail.Store(false)
				_, err = h.policy.Resume(h.ctx)
				require.NoError(t, err)
				result, err = h.service.CorrectContentType(h.ctx, article.ID, "research", false)
				require.NoError(t, err)
				require.Equal(t, types.ProcessingModeFull, result.ProcessingMode)
				require.Equal(t, "research", result.ContentType)
				require.Equal(t, types.ParseStatusCompleted, result.ParseStatus)
			})
		}
	}
}
