package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
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
	if old.IsArchivedSource() || old.IsSourceUpdateCandidate() {
		return false, fmt.Errorf("RSS source is not the current approved version")
	}
	if old.ParseStatus == types.ParseStatusPending || old.ParseStatus == types.ParseStatusProcessing || old.ParseStatus == types.ParseStatusFinalizing {
		return false, fmt.Errorf("RSS source is still processing; retry after it completes")
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
	if err := s.snapshotContextSource(ctx, old); err != nil {
		return false, fmt.Errorf("preserve original evidence: %w", err)
	}
	staged := *payload
	staged.Status = types.ManualKnowledgeStatusDraft
	staged.SourceMetadata = make(map[string]string, len(payload.SourceMetadata))
	for key, value := range payload.SourceMetadata {
		if key != "external_id" {
			staged.SourceMetadata[key] = value
		}
	}
	staged.SourceMetadata[sourceUpdateCandidateKey] = "true"
	staged.SourceMetadata[sourceUpdateReplacesKey] = old.ID
	staged.SourceMetadata[sourceUpdateExternalIDKey] = payload.SourceMetadata["external_id"]
	staged.SourceMetadata[sourceUpdateDataSourceKey] = payload.SourceMetadata["datasource_id"]
	staged.SourceMetadata[sourceUpdateDeferPromoteKey] = "true"
	staged.SourceMetadata[sourceUpdateEnableStatusKey] = old.EnableStatus
	// Keep the legacy key during the rollout so any queued RSS task observes
	// the same candidate gate until all workers have been updated.
	staged.SourceMetadata["rss_update_candidate"] = "true"
	candidate, err := s.findPendingRSSUpdateCandidate(ctx, old, payload.SourceMetadata)
	if err != nil {
		return false, err
	}
	reusedCandidate := candidate != nil
	if !reusedCandidate {
		candidate, err = s.CreateKnowledgeFromManual(ctx, kb.ID, &staged, old.Channel)
		if err != nil {
			return false, err
		}
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
			if s.db != nil && s.db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
				if err := NewKnowledgeContextService(s.db, s.modelService).DeleteMessages(ctx, kb.ID, []string{candidate.ID}); err != nil {
					return err
				}
			}
		} else {
			if err := s.DeleteKnowledge(ctx, candidate.ID); err != nil {
				return err
			}
		}
		return s.repo.HardDeleteKnowledge(ctx, tenantID, candidate.ID)
	}
	if !reusedCandidate {
		candidate.ParseStatus = types.ParseStatusProcessing
		if err := s.repo.UpdateKnowledge(ctx, candidate); err != nil {
			return false, errors.Join(err, removeCandidate())
		}
		s.triggerManualProcessing(ctx, kb, candidate, staged.Content, true)
	}
	intake, err := candidate.IntakeMetadata()
	if err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	if intake != nil && intake.Decision == "rejected" {
		reason := intake.Reason
		if err := removeCandidate(); err != nil {
			return false, err
		}
		recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditAction("knowledge.source_update"), "knowledge", old.ID, types.AuditOutcomeDenied,
			map[string]any{"reason": reason, "result": "original_preserved"})
		if err := s.recordSourceUpdateIssue(ctx, old, reason); err != nil {
			return false, err
		}
		brief := []rune(reason)
		return true, fmt.Errorf("%w: %s; approved original preserved", errRSSUpdateRejected, string(brief[:min(len(brief), 200)]))
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
	if stringMetadata(candidate.Metadata)[sourceUpdatePreparedKey] != "true" {
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
			choice.UpdatedAt = time.Now()
			if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "knowledge_id"}, {Name: "tag_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"selected", "actor_id", "reason", "updated_at"}),
			}).Create(&choice).Error; err != nil {
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
		if err := mergeStringMetadataIntoKnowledge(candidate, map[string]string{sourceUpdatePreparedKey: "true"}); err != nil {
			return false, errors.Join(err, removeCandidate())
		}
		if err := s.repo.UpdateKnowledge(ctx, candidate); err != nil {
			return false, errors.Join(err, removeCandidate())
		}
	}

	var latest types.Knowledge
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", candidate.ID, tenantID).First(&latest).Error; err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	if err := removeKnowledgeMetadata(&latest, sourceUpdateDeferPromoteKey); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	if err := s.repo.UpdateKnowledge(ctx, &latest); err != nil {
		return false, errors.Join(err, removeCandidate())
	}
	candidate = &latest
	// Retain the old source, its images and chunks for existing citations. The
	// candidate becomes current and the old row archived in one transaction.
	if err := s.promoteSourceUpdateCandidate(ctx, candidate.ID); err != nil {
		return false, fmt.Errorf("promote RSS source update candidate %s; approved original preserved for retry: %w", candidate.ID, err)
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditAction("knowledge.source_update"), "knowledge", candidate.ID, types.AuditOutcomeSuccess,
		map[string]any{"replaced_knowledge_id": old.ID})
	return false, nil
}

const (
	sourceUpdateCandidateKey      = "source_update_candidate"
	sourceUpdateReplacesKey       = "source_update_replaces"
	sourceUpdateExternalIDKey     = "source_update_external_id"
	sourceUpdateDeferPromoteKey   = "source_update_defer_promote"
	sourceUpdateEnableStatusKey   = "source_update_enable_status"
	sourceUpdateDataSourceKey     = "source_update_datasource_id"
	sourceUpdateSubtreeKeepKey    = "source_update_subtree_keep"
	sourceUpdateSubtreeReplaceKey = "source_update_subtree_replace"
	sourceUpdatePreparedKey       = "source_update_prepared"
)

func stringMetadata(metadata types.JSON) map[string]string {
	result := make(map[string]string)
	var raw map[string]json.RawMessage
	if json.Unmarshal(metadata, &raw) != nil {
		return result
	}
	for key, value := range raw {
		switch key {
		case "intake", "manual", "source_archived", "superseded_by", "rss_update_candidate":
			continue
		}
		var text string
		if json.Unmarshal(value, &text) == nil {
			result[key] = text
		}
	}
	return result
}

func mergeStringMetadataIntoKnowledge(knowledge *types.Knowledge, metadata map[string]string) error {
	var raw map[string]json.RawMessage
	if len(knowledge.Metadata) > 0 {
		if err := json.Unmarshal(knowledge.Metadata, &raw); err != nil {
			return err
		}
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	for key, value := range metadata {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		raw[key] = encoded
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	knowledge.Metadata = types.JSON(encoded)
	return nil
}

func removeKnowledgeMetadata(knowledge *types.Knowledge, keys ...string) error {
	var raw map[string]json.RawMessage
	if len(knowledge.Metadata) > 0 {
		if err := json.Unmarshal(knowledge.Metadata, &raw); err != nil {
			return err
		}
	}
	for _, key := range keys {
		delete(raw, key)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	knowledge.Metadata = types.JSON(encoded)
	return nil
}

// PromoteSourceUpdateCandidate finalizes a reviewed source replacement. Its
// public shape lets the shared async finalizer reuse the existing service.
func (s *knowledgeService) PromoteSourceUpdateCandidate(ctx context.Context, id string) error {
	return s.promoteSourceUpdateCandidate(ctx, id)
}

func (s *knowledgeService) promoteSourceUpdateCandidate(ctx context.Context, id string) (retErr error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("source update promotion requires tenant context")
	}
	defer func() {
		if retErr != nil {
			s.recordSourceUpdatePromotionFailure(ctx, tenantID, id, retErr)
		}
	}()
	candidate, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !candidate.IsSourceUpdateCandidate() {
		return nil
	}
	if s.db == nil {
		return fmt.Errorf("source update promotion requires database")
	}
	if candidate.ParseStatus != types.ParseStatusCompleted {
		return fmt.Errorf("source update candidate %s is not complete", id)
	}
	intake, err := candidate.IntakeMetadata()
	if err != nil {
		return err
	}
	if intake != nil && intake.Decision != "approved" {
		return fmt.Errorf("source update candidate %s is not approved", id)
	}
	var candidateMeta map[string]json.RawMessage
	if err := json.Unmarshal(candidate.Metadata, &candidateMeta); err != nil {
		return err
	}
	var oldID string
	if err := json.Unmarshal(candidateMeta[sourceUpdateReplacesKey], &oldID); err != nil || oldID == "" || oldID == id {
		return fmt.Errorf("source update candidate %s has no valid replacement target", id)
	}
	if stringMetadata(candidate.Metadata)[sourceUpdateDeferPromoteKey] == "true" {
		return nil
	}
	if err := s.preserveSourceUpdateManualTags(ctx, oldID, candidate); err != nil {
		return fmt.Errorf("preserve manual tag corrections: %w", err)
	}

	withContext := s.db != nil && s.db.Migrator().HasTable(&types.KnowledgeEvidence{})
	if withContext {
		body, err := s.contextMessageBody(ctx, candidate)
		if err != nil {
			return fmt.Errorf("read source update candidate original: %w", err)
		}
		if strings.TrimSpace(body) == "" {
			return fmt.Errorf("source update candidate original is empty")
		}
		// Snapshot while the candidate is still hidden. Context queries and
		// workers reject candidate rows, so evidence cannot leak before commit.
		if _, err := NewKnowledgeContextService(s.db, s.modelService).snapshotFormalMessage(ctx, tenantID, candidate, body); err != nil {
			return fmt.Errorf("snapshot source update evidence: %w", err)
		}
	}

	var promotedKeep []string
	var datasourceID, externalID string
	var replacesSubtree bool
	_ = json.Unmarshal(candidateMeta[sourceUpdateDataSourceKey], &datasourceID)
	_ = json.Unmarshal(candidateMeta[sourceUpdateExternalIDKey], &externalID)
	var replaceText, keepJSON string
	_ = json.Unmarshal(candidateMeta[sourceUpdateSubtreeReplaceKey], &replaceText)
	_ = json.Unmarshal(candidateMeta[sourceUpdateSubtreeKeepKey], &keepJSON)
	replacesSubtree = strings.EqualFold(replaceText, "true")
	if keepJSON != "" {
		_ = json.Unmarshal([]byte(keepJSON), &promotedKeep)
	}
	var promoted bool
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current, old types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", id, tenantID, candidate.KnowledgeBaseID).First(&current).Error; err != nil {
			return err
		}
		if !current.IsSourceUpdateCandidate() {
			return nil // an earlier delivery already promoted it
		}
		if current.ParseStatus != types.ParseStatusCompleted {
			return fmt.Errorf("source update candidate %s is not complete", id)
		}
		intake, err := current.IntakeMetadata()
		if err != nil {
			return err
		}
		if intake != nil && intake.Decision != "approved" {
			return fmt.Errorf("source update candidate %s is not approved", id)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", oldID, tenantID, current.KnowledgeBaseID).First(&old).Error; err != nil {
			return fmt.Errorf("load replaced source: %w", err)
		}
		if old.IsArchivedSource() {
			oldExternal := old.GetMetadata()["superseded_by"]
			if oldExternal == current.ID {
				return nil
			}
			return fmt.Errorf("replacement source %s was already superseded", old.ID)
		}

		var nextMeta map[string]json.RawMessage
		if err := json.Unmarshal(current.Metadata, &nextMeta); err != nil {
			return err
		}
		_ = json.Unmarshal(nextMeta[sourceUpdateExternalIDKey], &externalID)
		for _, key := range []string{sourceUpdateCandidateKey, "rss_update_candidate", sourceUpdateReplacesKey, sourceUpdateExternalIDKey,
			sourceUpdateDeferPromoteKey, sourceUpdateEnableStatusKey, sourceUpdateDataSourceKey, sourceUpdateSubtreeKeepKey, sourceUpdateSubtreeReplaceKey,
			"source_update_prepared", "source_update_error", "source_update_error_at"} {
			delete(nextMeta, key)
		}
		if externalID != "" {
			nextMeta["external_id"], _ = json.Marshal(externalID)
		}
		encodedNext, err := json.Marshal(nextMeta)
		if err != nil {
			return err
		}
		var oldMeta map[string]json.RawMessage
		if err := json.Unmarshal(old.Metadata, &oldMeta); err != nil {
			return err
		}
		delete(oldMeta, "external_id")
		oldMeta["source_archived"], _ = json.Marshal(true)
		oldMeta["superseded_by"], _ = json.Marshal(current.ID)
		encodedOld, err := json.Marshal(oldMeta)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ?", old.ID, tenantID).
			Updates(map[string]any{"metadata": types.JSON(encodedOld), "enable_status": "disabled", "updated_at": now}).Error; err != nil {
			return err
		}
		enableStatus := old.EnableStatus
		if enableStatus != "enabled" && enableStatus != "disabled" {
			enableStatus = "enabled"
		}
		if err := tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ?", current.ID, tenantID).
			Updates(map[string]any{"metadata": types.JSON(encodedNext), "enable_status": enableStatus, "error_message": "", "updated_at": now}).Error; err != nil {
			return err
		}
		promoted = true
		if !withContext {
			return nil
		}
		var replacementEvidence types.KnowledgeEvidence
		if err := tx.Where("knowledge_id = ? AND tenant_id = ? AND knowledge_base_id = ? AND status = ?", current.ID, tenantID, current.KnowledgeBaseID, types.KnowledgeEvidenceCurrent).
			Order("version DESC").First(&replacementEvidence).Error; err != nil {
			return fmt.Errorf("replacement evidence was not snapshotted: %w", err)
		}
		var oldEvidence []types.KnowledgeEvidence
		if err := tx.Where("knowledge_id = ? AND tenant_id = ? AND knowledge_base_id = ? AND status = ?", old.ID, tenantID, old.KnowledgeBaseID, types.KnowledgeEvidenceCurrent).Find(&oldEvidence).Error; err != nil {
			return err
		}
		affectedEvents := make(map[string]types.KnowledgeEvent)
		for _, evidence := range oldEvidence {
			if err := tx.Model(&types.KnowledgeEvidence{}).Where("id = ? AND tenant_id = ?", evidence.ID, tenantID).
				Updates(map[string]any{"status": types.KnowledgeEvidenceSuperseded, "superseded_by_id": replacementEvidence.ID}).Error; err != nil {
				return err
			}
			if err := tx.Model(&types.KnowledgeFact{}).Where("tenant_id = ? AND knowledge_base_id = ? AND evidence_id = ? AND status IN ?", tenantID, old.KnowledgeBaseID, evidence.ID,
				[]string{types.KnowledgeFactCurrent, types.KnowledgeFactCorrected}).Update("status", types.KnowledgeFactSuperseded).Error; err != nil {
				return err
			}
			var links []types.KnowledgeEventEvidence
			if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND evidence_id = ?", tenantID, old.KnowledgeBaseID, evidence.ID).Find(&links).Error; err != nil {
				return err
			}
			for _, link := range links {
				var event types.KnowledgeEvent
				if err := tx.Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", link.EventID, tenantID, old.KnowledgeBaseID).First(&event).Error; err == nil {
					affectedEvents[event.ID] = event
				} else if err != gorm.ErrRecordNotFound {
					return err
				}
			}
		}
		for _, event := range affectedEvents {
			if err := updateContextEventHeat(tx, tenantID, old.KnowledgeBaseID, &event); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if promoted && replacesSubtree && datasourceID != "" && externalID != "" {
		(&DataSourceService{knowledgeService: s}).sweepStaleSubtree(ctx,
			&types.DataSource{TenantID: tenantID, KnowledgeBaseID: candidate.KnowledgeBaseID, ID: datasourceID},
			&types.FetchedItem{ExternalID: externalID, ReplacesSubtree: true, SubtreeKeep: promotedKeep})
	}
	if promoted {
		var current types.Knowledge
		if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&current).Error; err != nil {
			logger.Warnf(ctx, "load promoted source update %s for downstream refresh: %v", id, err)
		} else if current.EnableStatus == "enabled" {
			s.queueContextSource(ctx, &current)
			if s.kbService != nil {
				kb, err := s.kbService.GetKnowledgeBaseByID(ctx, current.KnowledgeBaseID)
				if err != nil {
					logger.Warnf(ctx, "load knowledge base for promoted source update %s: %v", id, err)
				} else if kb != nil && kb.IndexingStrategy.WikiEnabled {
					if _, err := EnqueueWikiIngest(ctx, s.task, s.taskPendingRepo, tenantID, current.KnowledgeBaseID, current.ID); err != nil {
						logger.Warnf(ctx, "enqueue wiki refresh for promoted source update %s: %v", id, err)
					}
				}
			}
		}
	}
	return nil
}

func (s *knowledgeService) preserveSourceUpdateManualTags(ctx context.Context, oldID string, candidate *types.Knowledge) error {
	if s.db == nil || !s.db.Migrator().HasTable(&types.KnowledgeTagRelation{}) || !s.db.Migrator().HasTable(&types.KnowledgeTag{}) {
		return nil
	}
	oldTags, err := s.repo.GetKnowledgeTags(ctx, []string{oldID})
	if err != nil {
		return err
	}
	candidateTags, err := s.repo.GetKnowledgeTags(ctx, []string{candidate.ID})
	if err != nil {
		return err
	}
	final := make(map[string]bool)
	for _, tag := range candidateTags[candidate.ID] {
		final[tag.ID] = true
	}
	for _, tag := range oldTags[oldID] {
		if !managedMessageTag(tag.Kind) {
			final[tag.ID] = true
		}
	}
	var decisions []types.KnowledgeTagManualDecision
	if s.db.Migrator().HasTable(&types.KnowledgeTagManualDecision{}) {
		if err := s.db.WithContext(ctx).Where("knowledge_id = ?", oldID).Find(&decisions).Error; err != nil {
			return err
		}
	}
	for _, decision := range decisions {
		if decision.Selected {
			final[decision.TagID] = true
		} else {
			delete(final, decision.TagID)
		}
	}
	ids := make([]string, 0, len(final))
	for id := range final {
		ids = append(ids, id)
	}
	if err := s.repo.SetKnowledgeTags(ctx, candidate.ID, ids); err != nil {
		return err
	}
	if len(decisions) == 0 || !s.db.Migrator().HasTable(&types.KnowledgeTagManualDecision{}) {
		return nil
	}
	for i := range decisions {
		decisions[i].KnowledgeID = candidate.ID
		decisions[i].UpdatedAt = time.Now()
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "knowledge_id"}, {Name: "tag_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"selected", "actor_id", "reason", "updated_at"}),
	}).Create(&decisions).Error
}

func (s *knowledgeService) recordSourceUpdatePromotionFailure(ctx context.Context, tenantID uint64, id string, cause error) {
	if s.db == nil || id == "" || cause == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var current types.Knowledge
	if err := s.db.WithContext(writeCtx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&current).Error; err != nil || !current.IsSourceUpdateCandidate() {
		return
	}
	message := strings.TrimSpace(cause.Error())
	if len(message) > 1000 {
		message = message[:1000]
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(current.Metadata, &metadata); err != nil {
		logger.Warnf(ctx, "cannot record source update failure for %s: invalid metadata: %v", id, err)
		return
	}
	errorValue, _ := json.Marshal(message)
	timeValue, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
	metadata["source_update_error"] = errorValue
	metadata["source_update_error_at"] = timeValue
	encoded, err := json.Marshal(metadata)
	if err != nil {
		logger.Warnf(ctx, "cannot serialize source update failure for %s: %v", id, err)
		return
	}
	if err := s.db.WithContext(writeCtx).Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]any{"metadata": types.JSON(encoded), "error_message": message, "updated_at": time.Now()}).Error; err != nil {
		logger.Warnf(ctx, "cannot persist source update failure for %s: %v", id, err)
	}
}

func sourceUpdatePromotionCallback(svc interfaces.KnowledgeService) func(context.Context, string) error {
	promoter, ok := svc.(interface {
		PromoteSourceUpdateCandidate(context.Context, string) error
	})
	if !ok {
		return nil
	}
	return promoter.PromoteSourceUpdateCandidate
}

func (s *knowledgeService) recordSourceUpdateIssue(ctx context.Context, old *types.Knowledge, reason string) error {
	if s.db == nil || !s.db.Migrator().HasTable(&types.KnowledgeAnomaly{}) {
		return nil
	}
	item := &types.KnowledgeAnomaly{ID: uuid.NewString(), TenantID: old.TenantID, KnowledgeBaseID: old.KnowledgeBaseID,
		KnowledgeIDs: []string{old.ID}, Kind: "outdated", Description: "Source update failed intake review; approved evidence retained: " + reason,
		ProposalType: "none", Status: "pending", ReportedBy: "system:source-update", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return s.db.WithContext(ctx).Create(item).Error
}

func (s *knowledgeService) findPendingRSSUpdateCandidate(
	ctx context.Context, old *types.Knowledge, sourceMetadata map[string]string,
) (*types.Knowledge, error) {
	rows, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, old.TenantID, old.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !row.IsSourceUpdateCandidate() || row.ParseStatus != types.ParseStatusCompleted {
			continue
		}
		metadata := stringMetadata(row.Metadata)
		if metadata[sourceUpdateReplacesKey] != old.ID ||
			metadata[sourceUpdateExternalIDKey] != sourceMetadata["external_id"] ||
			metadata[sourceUpdateDataSourceKey] != sourceMetadata["datasource_id"] ||
			metadata["content_sha256"] != sourceMetadata["content_sha256"] ||
			metadata[sourceUpdatePreparedKey] != "true" {
			continue
		}
		intake, err := row.IntakeMetadata()
		if err != nil {
			return nil, err
		}
		if intake != nil && intake.Decision == "approved" {
			return row, nil
		}
	}
	return nil, nil
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
