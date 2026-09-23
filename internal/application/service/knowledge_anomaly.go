package service

import (
	"context"
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
	KnowledgeIDs        []string `json:"knowledge_ids"`
	Kind                string   `json:"kind"`
	Description         string   `json:"description"`
	ProposalType        string   `json:"proposal_type"`
	ProposedTagIDs      []string `json:"proposed_tag_ids"`
	ProposedDeleteTagID string   `json:"proposed_delete_tag_id"`
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

func (s *KnowledgeAnomalyService) List(ctx context.Context, kbID string) ([]types.KnowledgeAnomaly, error) {
	kb, err := s.scopedKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	items := make([]types.KnowledgeAnomaly, 0)
	err = s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", kb.TenantID, kb.ID).
		Order("created_at DESC").Limit(100).Find(&items).Error
	return items, err
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
	if input.ProposalType != "none" && input.ProposalType != "set_tags" && input.ProposalType != "delete_tag" {
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
	if len(ids) > 8 || (len(ids) == 0 && input.ProposalType != "delete_tag") || (input.ProposalType == "set_tags" && len(ids) != 1) {
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
	proposalTags := make([]string, 0, len(input.ProposedTagIDs))
	if input.ProposalType == "set_tags" {
		tagSeen := make(map[string]bool, len(input.ProposedTagIDs))
		for _, id := range input.ProposedTagIDs {
			id = strings.TrimSpace(id)
			if id != "" && !tagSeen[id] {
				tagSeen[id] = true
				proposalTags = append(proposalTags, id)
			}
		}
		if len(proposalTags) > 100 {
			return nil, werrors.NewBadRequestError("too many proposed tags")
		}
		if len(proposalTags) > 0 {
			var count int64
			if err := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
				Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ?", kb.TenantID, kb.ID, proposalTags).Count(&count).Error; err != nil {
				return nil, err
			}
			if count != int64(len(proposalTags)) {
				return nil, werrors.NewBadRequestError("proposed tags do not belong to this knowledge base")
			}
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
		ProposalType: input.ProposalType, ProposedTagIDs: proposalTags, ProposedDeleteTagID: deleteTagID,
		Status: "pending", ReportedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(item).Error; err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKnowledgeAnomalyReported, "knowledge_anomaly", item.ID,
		types.AuditOutcomeSuccess, map[string]any{"kind": item.Kind, "knowledge_ids": item.KnowledgeIDs})
	return item, nil
}

// Resolve accepts or rejects a suggestion. Only the KB creator can decide;
// a reviewer with read access cannot use this path to mutate knowledge.
func (s *KnowledgeAnomalyService) Resolve(ctx context.Context, kbID, anomalyID, action, note string) (*types.KnowledgeAnomaly, error) {
	kb, err := s.scopedKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	actorID, ok := types.UserIDFromContext(ctx)
	if !ok || actorID != kb.CreatorID || types.IsSyntheticUserID(actorID) {
		return nil, werrors.NewForbiddenError("only the knowledge base creator can decide anomalies")
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
	if item.Status != "pending" {
		return nil, werrors.NewBadRequestError("anomaly already decided")
	}
	status, result, expectedStatus := action, "", "pending"
	if action == "apply" {
		if item.ProposalType != "set_tags" && item.ProposalType != "delete_tag" {
			return nil, werrors.NewBadRequestError("anomaly has no executable proposal")
		}
		if item.ProposalType == "set_tags" && len(item.KnowledgeIDs) != 1 {
			return nil, werrors.NewBadRequestError("tag proposal requires one message")
		}
		claim := s.db.WithContext(ctx).Model(&types.KnowledgeAnomaly{}).
			Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND status = ?", anomalyID, kb.TenantID, kb.ID, "pending").
			Update("status", "applying")
		if claim.Error != nil {
			return nil, claim.Error
		}
		if claim.RowsAffected != 1 {
			return nil, werrors.NewBadRequestError("anomaly already decided")
		}
		expectedStatus = "applying"
		var applyErr error
		switch item.ProposalType {
		case "set_tags":
			reason := note
			if reason == "" {
				runes := []rune(item.Description)
				reason = string(runes[:min(len(runes), 500)])
			}
			applyErr = s.knowledge.UpdateKnowledgeTagBatch(ctx, kb.ID, map[string][]string{item.KnowledgeIDs[0]: item.ProposedTagIDs}, reason)
			result = "tags_updated"
		case "delete_tag":
			applyErr = s.tags.DeleteTag(ctx, item.ProposedDeleteTagID, false, false, nil)
			result = "tag_deleted"
		}
		if applyErr != nil {
			s.db.WithContext(ctx).Model(&types.KnowledgeAnomaly{}).Where("id = ? AND status = ?", item.ID, "applying").Update("status", "pending")
			return nil, applyErr
		}
		status = "applied"
	}
	now := time.Now()
	update := map[string]any{"status": status, "decided_by": actorID, "decision_note": note, "result": result, "decided_at": now, "updated_at": now}
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
		types.AuditOutcomeSuccess, map[string]any{"action": action, "knowledge_ids": item.KnowledgeIDs, "result": result, "note": note})
	return &item, nil
}
