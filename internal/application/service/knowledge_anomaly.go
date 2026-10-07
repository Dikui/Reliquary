package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type KnowledgeAnomalyInput struct {
	KnowledgeIDs         []string `json:"knowledge_ids"`
	Kind                 string   `json:"kind"`
	Description          string   `json:"description"`
	ProposalType         string   `json:"proposal_type"`
	ProposedTagIDs       []string `json:"proposed_tag_ids"`
	ProposedRemoveTagIDs []string `json:"proposed_remove_tag_ids"`
	ProposedDeleteTagID  string   `json:"proposed_delete_tag_id"`
}

type KnowledgeAnomalyService struct {
	db        *gorm.DB
	knowledge interfaces.KnowledgeService
	tags      interfaces.KnowledgeTagService
	audit     interfaces.AuditLogService
}

func NewKnowledgeAnomalyService(db *gorm.DB, knowledge interfaces.KnowledgeService, tags interfaces.KnowledgeTagService, audit interfaces.AuditLogService) *KnowledgeAnomalyService {
	return &KnowledgeAnomalyService{db: db, knowledge: knowledge, tags: tags, audit: audit}
}

func (s *KnowledgeAnomalyService) scopedKB(ctx context.Context, kbID string) (*types.KnowledgeBase, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, werrors.NewUnauthorizedError("workspace required")
	}
	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Select("id", "tenant_id", "creator_id").
		Where("id = ? AND tenant_id = ?", kbID, tenantID).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("knowledge base not found")
		}
		return nil, err
	}
	return &kb, nil
}

func (s *KnowledgeAnomalyService) List(ctx context.Context, kbID string, offset int) ([]types.KnowledgeAnomaly, bool, error) {
	kb, err := s.scopedKB(ctx, kbID)
	if err != nil {
		return nil, false, err
	}
	items := make([]types.KnowledgeAnomaly, 0)
	err = s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", kb.TenantID, kb.ID).
		Order("created_at DESC, id DESC").Offset(offset).Limit(51).Find(&items).Error
	if err != nil {
		return nil, false, err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	return items, more, nil
}

// ForMessages returns source conflicts relevant to retrieved messages, so a
// later answer can respect the owner's decision without rereading all reports.
func (s *KnowledgeAnomalyService) ForMessages(ctx context.Context, kbIDs, messageIDs []string) ([]types.KnowledgeAnomaly, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, werrors.NewUnauthorizedError("workspace required")
	}
	if s.db == nil || len(kbIDs) == 0 || len(messageIDs) == 0 {
		return nil, nil
	}
	// ponytail: scan conflicts in the selected bases; add a message-ID index if this becomes slow.
	var candidates []types.KnowledgeAnomaly
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ? AND kind = ? AND status IN ?",
		tenantID, kbIDs, "source_conflict", []string{"pending", "applying", "resolved", "applied"}).
		Order("updated_at DESC").Find(&candidates).Error; err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(messageIDs))
	for _, id := range messageIDs {
		wanted[id] = true
	}
	matched := make([]types.KnowledgeAnomaly, 0)
	for _, candidate := range candidates {
		for _, id := range candidate.KnowledgeIDs {
			if wanted[id] {
				matched = append(matched, candidate)
				break
			}
		}
		if len(matched) == 20 {
			break
		}
	}
	return matched, nil
}

func (s *KnowledgeAnomalyService) Create(ctx context.Context, kbID string, input KnowledgeAnomalyInput) (*types.KnowledgeAnomaly, error) {
	kb, err := s.scopedKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	input.Kind = strings.TrimSpace(input.Kind)
	input.Description = strings.TrimSpace(input.Description)
	input.ProposalType = strings.TrimSpace(input.ProposalType)
	if input.ProposalType == "" {
		input.ProposalType = "none"
	}
	validKind := map[string]bool{"suspected_error": true, "outdated": true, "source_conflict": true, "manual_conflict": true, "tag_deletion": true}
	if !validKind[input.Kind] || input.Description == "" || len([]rune(input.Description)) > 2000 {
		return nil, werrors.NewBadRequestError("invalid anomaly kind or description")
	}
	if input.ProposalType != "none" && input.ProposalType != "change_tags" && input.ProposalType != "delete_tag" {
		return nil, werrors.NewBadRequestError("invalid correction proposal")
	}
	ids := make([]string, 0, len(input.KnowledgeIDs))
	seen := make(map[string]bool, len(input.KnowledgeIDs))
	for _, id := range input.KnowledgeIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > 8 || (len(ids) == 0 && input.ProposalType != "delete_tag") {
		return nil, werrors.NewBadRequestError("invalid related messages")
	}
	if len(ids) > 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&types.Knowledge{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND parse_status IN ?", kb.TenantID, kb.ID, ids,
				[]string{types.ParseStatusFinalizing, types.ParseStatusCompleted}).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(ids)) {
			return nil, werrors.NewBadRequestError("related messages must be formal messages in this knowledge base")
		}
	}
	proposalTags, removeTags := []string{}, []string{}
	if input.ProposalType == "change_tags" {
		proposalTags = normalizeAnomalyTagIDs(input.ProposedTagIDs)
		removeTags = normalizeAnomalyTagIDs(input.ProposedRemoveTagIDs)
		if len(proposalTags)+len(removeTags) == 0 || len(proposalTags)+len(removeTags) > 100 {
			return nil, werrors.NewBadRequestError("invalid proposed tag changes")
		}
		all := append(append([]string{}, proposalTags...), removeTags...)
		if len(normalizeAnomalyTagIDs(all)) != len(all) {
			return nil, werrors.NewBadRequestError("a tag cannot be both added and removed")
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ?", kb.TenantID, kb.ID, all).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(all)) {
			return nil, werrors.NewBadRequestError("proposed tags do not belong to this knowledge base")
		}
	}
	deleteTagID := ""
	if input.ProposalType == "delete_tag" {
		deleteTagID = strings.TrimSpace(input.ProposedDeleteTagID)
		var count int64
		if deleteTagID == "" {
			return nil, werrors.NewBadRequestError("tag deletion requires a tag")
		}
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", kb.TenantID, kb.ID, deleteTagID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, werrors.NewBadRequestError("tag does not belong to this knowledge base")
		}
	}
	actorID, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, werrors.NewUnauthorizedError("reporter required")
	}
	now := time.Now()
	item := &types.KnowledgeAnomaly{
		ID: uuid.NewString(), TenantID: kb.TenantID, KnowledgeBaseID: kb.ID,
		KnowledgeIDs: ids, Kind: input.Kind, Description: input.Description,
		ProposalType: input.ProposalType, ProposedTagIDs: proposalTags, ProposedRemoveTagIDs: removeTags,
		ProposedDeleteTagID: deleteTagID, AppliedChanges: map[string]types.KnowledgeAnomalyTagChange{},
		Status: "pending", ReportedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(item).Error; err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKnowledgeAnomalyReported, "knowledge_anomaly", item.ID,
		types.AuditOutcomeSuccess, map[string]any{"kind": item.Kind, "knowledge_ids": item.KnowledgeIDs})
	return item, nil
}

func normalizeAnomalyTagIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Resolve accepts or rejects a suggestion. A team editor can decide;
// a reviewer with read access cannot use this path to mutate knowledge.
func (s *KnowledgeAnomalyService) Resolve(ctx context.Context, kbID, anomalyID, action, note string) (*types.KnowledgeAnomaly, error) {
	kb, err := s.scopedKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	actorID, ok := types.UserIDFromContext(ctx)
	if !ok || !types.TenantRoleFromContext(ctx).HasPermission(types.TenantRoleContributor) || types.IsSyntheticUserID(actorID) {
		return nil, werrors.NewForbiddenError("a team editor is required to decide anomalies")
	}
	if action != "apply" && action != "resolved" && action != "dismiss" {
		return nil, werrors.NewBadRequestError("invalid decision")
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		return nil, werrors.NewBadRequestError("decision note too long")
	}
	var item types.KnowledgeAnomaly
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", anomalyID, kb.TenantID, kb.ID).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("anomaly not found")
		}
		return nil, err
	}
	retrying := action == "apply" && item.Status == "applying"
	if item.Status != "pending" && !retrying {
		return nil, werrors.NewBadRequestError("anomaly already decided")
	}
	if item.Kind == "source_conflict" && action == "resolved" && note == "" {
		return nil, werrors.NewBadRequestError("source conflict resolution requires an owner decision")
	}
	if retrying && time.Since(item.UpdatedAt) < 2*time.Minute {
		return nil, werrors.NewBadRequestError("anomaly is still applying; retry after two minutes")
	}
	if retrying && note == "" {
		note = item.DecisionNote
	}
	status, result, expectedStatus := action, "", "pending"
	if action == "apply" {
		if item.ProposalType != "change_tags" && item.ProposalType != "delete_tag" {
			return nil, werrors.NewBadRequestError("anomaly has no executable proposal")
		}
		claim := s.db.WithContext(ctx).Model(&types.KnowledgeAnomaly{}).
			Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND status = ?", anomalyID, kb.TenantID, kb.ID, item.Status).
			Updates(map[string]any{"status": "applying", "updated_at": time.Now(), "decided_by": actorID, "decision_note": note})
		if claim.Error != nil {
			return nil, claim.Error
		}
		if claim.RowsAffected != 1 {
			return nil, werrors.NewBadRequestError("anomaly already decided")
		}
		expectedStatus = "applying"
		var applyErr error
		switch item.ProposalType {
		case "change_tags":
			reason := note
			if reason == "" {
				runes := []rune(item.Description)
				reason = string(runes[:min(len(runes), 500)])
			}
			current, getErr := s.knowledge.GetKnowledgeTags(ctx, item.KnowledgeIDs)
			if getErr != nil {
				applyErr = getErr
				break
			}
			updates := make(map[string][]string, len(item.KnowledgeIDs))
			plannedChanges := make(map[string]types.KnowledgeAnomalyTagChange, len(item.KnowledgeIDs))
			for _, id := range item.KnowledgeIDs {
				selected := make(map[string]bool)
				for _, tag := range current[id] {
					selected[tag.ID] = true
				}
				change := types.KnowledgeAnomalyTagChange{Added: []string{}, Removed: []string{}}
				for _, tagID := range item.ProposedTagIDs {
					if !selected[tagID] {
						selected[tagID] = true
						change.Added = append(change.Added, tagID)
					}
				}
				for _, tagID := range item.ProposedRemoveTagIDs {
					if selected[tagID] {
						delete(selected, tagID)
						change.Removed = append(change.Removed, tagID)
					}
				}
				finalIDs := make([]string, 0, len(selected))
				for tagID := range selected {
					finalIDs = append(finalIDs, tagID)
				}
				updates[id] = finalIDs
				plannedChanges[id] = change
			}
			if len(item.AppliedChanges) == 0 {
				item.AppliedChanges = plannedChanges
				changes, marshalErr := json.Marshal(item.AppliedChanges)
				if marshalErr != nil {
					applyErr = marshalErr
					break
				}
				if saveErr := s.db.WithContext(ctx).Model(&types.KnowledgeAnomaly{}).Where("id = ? AND status = ?", item.ID, "applying").Update("applied_changes", string(changes)).Error; saveErr != nil {
					applyErr = saveErr
					break
				}
			}
			applyErr = s.knowledge.UpdateKnowledgeTagBatch(ctx, kb.ID, updates, reason)
			result = "tags_updated"
		case "delete_tag":
			applyErr = s.tags.DeleteTag(ctx, item.ProposedDeleteTagID)
			if applyErr != nil && retrying {
				var remaining int64
				if countErr := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", item.ProposedDeleteTagID, kb.TenantID, kb.ID).Count(&remaining).Error; countErr == nil && remaining == 0 {
					applyErr = nil
				}
			}
			result = "tag_deleted"
		}
		if applyErr != nil {
			return nil, applyErr
		}
		status = "applied"
	}
	now := time.Now()
	changes, err := json.Marshal(item.AppliedChanges)
	if err != nil {
		return nil, err
	}
	update := map[string]any{"status": status, "decided_by": actorID, "decision_note": note, "result": result, "applied_changes": string(changes), "decided_at": now, "updated_at": now}
	decision := s.db.WithContext(ctx).Model(&types.KnowledgeAnomaly{}).
		Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND status = ?", anomalyID, kb.TenantID, kb.ID, expectedStatus).Updates(update)
	if decision.Error != nil {
		return nil, decision.Error
	}
	if decision.RowsAffected != 1 {
		return nil, werrors.NewBadRequestError("anomaly already decided")
	}
	item.Status, item.DecidedBy, item.DecisionNote, item.Result, item.DecidedAt, item.UpdatedAt = status, actorID, note, result, &now, now
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKnowledgeAnomalyDecided, "knowledge_anomaly", item.ID,
		types.AuditOutcomeSuccess, map[string]any{"action": action, "knowledge_ids": item.KnowledgeIDs, "result": result, "note": note, "changes": item.AppliedChanges, "deleted_tag_id": item.ProposedDeleteTagID})
	return &item, nil
}
