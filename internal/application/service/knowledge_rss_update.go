package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"reliquary/internal/types"
)

var errRSSUpdateRejected = errors.New("RSS article update rejected")

// ReviewRSSUpdate keeps the old article intact while a changed original is reviewed.
func (s *knowledgeService) ReviewRSSUpdate(ctx context.Context, oldID string, payload *types.ManualKnowledgePayload) (bool, error) {
	if payload == nil || payload.SourceMetadata["external_id"] == "" || payload.SourceMetadata["datasource_id"] == "" {
		return false, fmt.Errorf("RSS update needs a source identity")
	}
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	old, err := s.repo.GetKnowledgeByID(ctx, tenantID, oldID)
	if err != nil {
		return false, err
	}
	if old.Channel != types.ChannelRSS && old.Channel != types.ConnectorTypeGrokX {
		return false, fmt.Errorf("message is not a daily-source article")
	}
	var oldMeta map[string]json.RawMessage
	if err := json.Unmarshal(old.Metadata, &oldMeta); err != nil {
		return false, err
	}
	for _, key := range []string{"external_id", "datasource_id"} {
		var value string
		if json.Unmarshal(oldMeta[key], &value) != nil || value != payload.SourceMetadata[key] {
			return false, fmt.Errorf("RSS update source does not match")
		}
	}
	oldIntake, err := old.IntakeMetadata()
	if err != nil || oldIntake == nil || oldIntake.Decision != "approved" {
		return false, fmt.Errorf("RSS update requires an approved original")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, old.KnowledgeBaseID)
	if err != nil {
		return false, err
	}
	staged := *payload
	staged.Status = types.ManualKnowledgeStatusDraft
	staged.SourceMetadata = make(map[string]string, len(payload.SourceMetadata))
	for key, value := range payload.SourceMetadata {
		if key != "external_id" {
			staged.SourceMetadata[key] = value
		}
	}
	staged.SourceMetadata["rss_update_candidate"] = "true"
	candidate, err := s.CreateKnowledgeFromManual(ctx, kb.ID, &staged, old.Channel)
	if err != nil {
		return false, err
	}
	removeCandidate := func() error {
		intake, _ := candidate.IntakeMetadata()
		if intake != nil && intake.Decision == "rejected" {
			if err := s.clearIntakeSource(ctx, candidate); err != nil {
				return err
			}
			if err := s.repo.DeleteKnowledgeTagRelations(ctx, candidate.ID); err != nil {
				return err
			}
		} else if err := s.DeleteKnowledge(ctx, candidate.ID); err != nil {
			return err
		}
		return s.repo.HardDeleteKnowledge(ctx, tenantID, candidate.ID)
	}
	candidate.ParseStatus = types.ParseStatusProcessing
	if err := s.repo.UpdateKnowledge(ctx, candidate); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	s.triggerManualProcessing(ctx, kb, candidate, staged.Content, true)
	intake, err := candidate.IntakeMetadata()
	if err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	if intake != nil && intake.Decision == "rejected" {
		reason := intake.Reason
		if err := removeCandidate(); err != nil {
			return false, err
		}
		if err := s.DeleteKnowledge(ctx, old.ID); err != nil {
			return false, err
		}
		if err := s.repo.HardDeleteKnowledge(ctx, tenantID, old.ID); err != nil {
			return false, err
		}
		recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditAction("knowledge.source_update"), "knowledge", old.ID, types.AuditOutcomeDenied,
			map[string]any{"reason": reason, "result": "original_deleted"})
		brief := []rune(reason)
		return true, fmt.Errorf("%w: %s; original deleted", errRSSUpdateRejected, string(brief[:min(len(brief), 200)]))
	}
	if intake == nil || intake.Decision != "approved" || candidate.ProcessedAt == nil || candidate.ParseStatus == types.ParseStatusFailed || candidate.EnableStatus != "disabled" {
		return false, errors.Join(fmt.Errorf("RSS update review or indexing failed"), removeCandidate())
	}
	// Enrichment writes the whole message row. Promote only after those writes
	// finish, then work from the latest row so they cannot hide the replacement.
	candidate, err = s.waitRSSUpdateCandidate(ctx, tenantID, candidate.ID)
	if err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	// Keep the candidate disabled while classification and manual decisions run.
	classifier := &KnowledgeAutoTagService{db: s.db, knowledgeRepo: s.repo, tagRepo: s.tagRepo,
		kbService: s.kbService, chunkService: s.chunkService, modelService: s.modelService}
	if err := classifier.tagMessage(ctx, kb, candidate); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	oldTags, err := s.repo.GetKnowledgeTags(ctx, []string{old.ID})
	if err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	newTags, err := s.repo.GetKnowledgeTags(ctx, []string{candidate.ID})
	if err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	var choices []types.KnowledgeTagManualDecision
	if err := s.db.WithContext(ctx).Where("knowledge_id = ?", old.ID).Find(&choices).Error; err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	final := make(map[string]bool)
	for _, tag := range newTags[candidate.ID] {
		final[tag.ID] = true
	}
	for _, tag := range oldTags[old.ID] {
		if !managedMessageTag(tag.Kind) {
			final[tag.ID] = true
		}
	}
	toAdd, toRemove := []string{}, []string{}
	for _, choice := range choices {
		if choice.Selected {
			if !final[choice.TagID] {
				toRemove = append(toRemove, choice.TagID)
			}
			final[choice.TagID] = true
		} else {
			if final[choice.TagID] {
				toAdd = append(toAdd, choice.TagID)
			}
			delete(final, choice.TagID)
		}
		choice.KnowledgeID = candidate.ID
		if err := s.db.WithContext(ctx).Create(&choice).Error; err != nil {
			return false, errors.Join(err, removeCandidate())
		}
	}
	ids := make([]string, 0, len(final))
	for id := range final {
		ids = append(ids, id)
	}
	if err := s.repo.SetKnowledgeTags(ctx, candidate.ID, ids); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	delete(staged.SourceMetadata, "rss_update_candidate")
	staged.SourceMetadata["external_id"] = payload.SourceMetadata["external_id"]
	var candidateMeta map[string]json.RawMessage
	if err := json.Unmarshal(candidate.Metadata, &candidateMeta); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	delete(candidateMeta, "rss_update_candidate")
	candidateMeta["status"], _ = json.Marshal(types.ManualKnowledgeStatusPublish)
	for _, key := range []string{"external_id", "content_sha256"} {
		candidateMeta[key], _ = json.Marshal(staged.SourceMetadata[key])
	}
	metadata, err := json.Marshal(candidateMeta)
	if err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	candidate.Metadata, candidate.EnableStatus, candidate.UpdatedAt = types.JSON(metadata), "enabled", time.Now()
	if err := s.repo.UpdateKnowledge(ctx, candidate); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	if len(toAdd)+len(toRemove) > 0 {
		anomaly := &types.KnowledgeAnomaly{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kb.ID,
			KnowledgeIDs: []string{candidate.ID}, Kind: "manual_conflict", Description: "Updated original conflicts with manual tag corrections",
			ProposalType: "change_tags", ProposedTagIDs: toAdd, ProposedRemoveTagIDs: toRemove,
			AppliedChanges: map[string]types.KnowledgeAnomalyTagChange{},
			Status:         "pending", ReportedBy: "system:rss-update", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.db.WithContext(ctx).Create(anomaly).Error; err != nil {
			return false, errors.Join(err, removeCandidate())
		}
	}
	// Make the reviewed replacement available before deleting the old row: a
	// deletion failure may leave two copies, but must not remove the only copy.
	if err := s.DeleteKnowledge(ctx, old.ID); err != nil {
		return false, err
	}
	if err := s.repo.HardDeleteKnowledge(ctx, tenantID, old.ID); err != nil {
		return false, err
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditAction("knowledge.source_update"), "knowledge", candidate.ID, types.AuditOutcomeSuccess,
		map[string]any{"replaced_knowledge_id": old.ID})
	return false, nil
}

func (s *knowledgeService) waitRSSUpdateCandidate(ctx context.Context, tenantID uint64, id string) (*types.Knowledge, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		candidate, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		switch candidate.ParseStatus {
		case types.ParseStatusCompleted:
			return candidate, nil
		case types.ParseStatusFailed, types.ParseStatusCancelled, types.ParseStatusRejected, types.ParseStatusDeleting:
			return nil, fmt.Errorf("RSS update candidate ended in %s", candidate.ParseStatus)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
