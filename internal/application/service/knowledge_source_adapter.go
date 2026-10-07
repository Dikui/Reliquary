package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

// recordSourceAdapterFailures persists recoverable parsing failures before intake.
// This also records discovery failures that have no ingestible message yet.
func (s *knowledgeService) recordSourceAdapterFailures(ctx context.Context, tenantID uint64, kbID string, failures []types.SourceAdapterFailure) error {
	return recordSourceAdapterFailures(ctx, s.db, tenantID, kbID, failures)
}

func recordSourceAdapterFailures(ctx context.Context, db *gorm.DB, tenantID uint64, kbID string, failures []types.SourceAdapterFailure) error {
	if len(failures) == 0 {
		return nil
	}
	if db == nil {
		return fmt.Errorf("source adapter anomaly database unavailable")
	}
	now := time.Now()
	for _, failure := range failures {
		reason := strings.TrimSpace(failure.Reason)
		if reason == "" {
			reason = "site adapter could not restore the original page"
		}
		anomaly := &types.KnowledgeAnomaly{
			ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID,
			KnowledgeIDs: []string{}, Kind: "source_adapter_failed", Description: reason,
			Site: failure.Site, SourceURL: failure.URL, Reason: reason, Adapter: failure.Adapter,
			ProposalType: "none", ProposedTagIDs: []string{}, ProposedRemoveTagIDs: []string{},
			AppliedChanges: map[string]types.KnowledgeAnomalyTagChange{}, Status: "pending",
			ReportedBy: "", CreatedAt: now, UpdatedAt: now,
		}
		if err := db.WithContext(ctx).Create(anomaly).Error; err != nil {
			return fmt.Errorf("persist source adapter failure: %w", err)
		}
	}
	return nil
}
