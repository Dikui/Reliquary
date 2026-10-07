package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type KnowledgeReprocessResult struct {
	Processed int      `json:"processed"`
	Updated   int      `json:"updated"`
	Flagged   int      `json:"flagged"`
	Conflicts int      `json:"conflicts"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors"`
}

// Reprocess is an explicit owner action over one set of messages or a concrete
// topic/object/event. Screening failures are flagged for review; they do not
// silently delete an unchanged original. Manual tag decisions stay authoritative.
func (s *KnowledgeStrategyService) Reprocess(ctx context.Context, kbID string, messageIDs []string, tagID string) (*KnowledgeReprocessResult, error) {
	kb, _, err := s.scopedKB(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	if (len(messageIDs) == 0) == (tagID == "") {
		return nil, werrors.NewBadRequestError("choose message_ids or one topic/object/event tag")
	}
	ids := make([]string, 0)
	base := s.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND parse_status IN ?", kb.TenantID, kb.ID,
			[]string{types.ParseStatusFinalizing, types.ParseStatusCompleted})
	if tagID != "" {
		var tag types.KnowledgeTag
		if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND kind IN ?",
			tagID, kb.TenantID, kb.ID, []string{types.TagKindTopic, types.TagKindObject, types.TagKindEvent}).First(&tag).Error; err != nil {
			return nil, werrors.NewBadRequestError("scope tag must be a topic, object or event in this knowledge base")
		}
		if err := base.Joins("JOIN knowledge_tag_relations AS rel ON rel.knowledge_id = knowledges.id").
			Where("rel.tag_id = ?", tagID).Order("knowledges.id").Pluck("knowledges.id", &ids).Error; err != nil {
			return nil, err
		}
	} else {
		seen := make(map[string]bool)
		for _, id := range messageIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				return nil, werrors.NewBadRequestError("message ID required")
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		var count int64
		if err := base.Where("id IN ?", ids).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(ids)) {
			return nil, werrors.NewBadRequestError("all messages must be formal and in this knowledge base")
		}
	}
	result := &KnowledgeReprocessResult{Errors: []string{}}
	if len(ids) == 0 {
		return result, nil
	}
	reviewModel, err := GetTierChatModel(ctx, s.db, s.models, kb, types.ModelTierLight)
	if err != nil {
		return nil, err
	}
	tagModel := reviewModel
	rules, err := activeKnowledgeStrategyRules(ctx, s.db, kb.TenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if rules == nil {
		rules = &KnowledgeStrategyRules{}
		if kb.AutoTagConfig != nil {
			rules.Tags = kb.AutoTagConfig.Policy
		}
	}
	start := time.Now()
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Processed++
		status, err := s.reprocessMessage(ctx, kb, id, rules, reviewModel, tagModel)
		if err != nil {
			result.Failed++
			if len(result.Errors) < 20 {
				brief := []rune(err.Error())
				result.Errors = append(result.Errors, id+": "+string(brief[:min(len(brief), 200)]))
			}
			continue
		}
		switch status {
		case "updated":
			result.Updated++
		case "flagged":
			result.Flagged++
		case "conflict":
			result.Updated++
			result.Conflicts++
		case "conflict_unchanged":
			result.Conflicts++
		}
	}
	status := types.AuditOutcomeSuccess
	if result.Failed > 0 {
		status = types.AuditOutcomePartial
	}
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKnowledgeReprocessed, "knowledge_base", kb.ID,
		status, map[string]any{"scope_tag_id": tagID, "scope_count": len(ids), "processed": result.Processed, "updated": result.Updated,
			"flagged": result.Flagged, "conflicts": result.Conflicts, "failed": result.Failed, "errors": result.Errors,
			"duration_ms": time.Since(start).Milliseconds(), "model_id": kb.SummaryModelID})
	return result, nil
}

func (s *KnowledgeStrategyService) reprocessMessage(ctx context.Context, kb *types.KnowledgeBase, id string, rules *KnowledgeStrategyRules, reviewModel, tagModel chat.Chat) (string, error) {
	var message types.Knowledge
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND parse_status IN ?",
		id, kb.TenantID, kb.ID, []string{types.ParseStatusFinalizing, types.ParseStatusCompleted}).First(&message).Error; err != nil {
		return "", err
	}
	intake, err := message.IntakeMetadata()
	if err != nil {
		return "", err
	}
	if intake == nil || intake.Decision != "approved" {
		return "", fmt.Errorf("message lacks an approved original")
	}
	file, _, err := s.knowledge.GetKnowledgeFile(ctx, id)
	if err != nil {
		return "", err
	}
	body, readErr := io.ReadAll(io.LimitReader(file, 800001))
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(body) > 800000 || strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("original is empty or too large")
	}
	if invalidIntakeEvidence(string(body), intake.SourceInfo, &message) {
		_, err := NewKnowledgeAnomalyService(s.db, s.knowledge, nil, s.audit).Create(ctx, kb.ID, KnowledgeAnomalyInput{
			KnowledgeIDs: []string{id}, Kind: "suspected_error", Description: "Historical strategy recheck: missing original or required source evidence"})
		return "flagged", err
	}
	approved, reason, labels, err := reviewAndClassifyMessage(ctx, reviewModel, rules, message.Channel, string(body), intake.SourceInfo)
	if err != nil {
		return "", err
	}
	if !approved {
		_, err := NewKnowledgeAnomalyService(s.db, s.knowledge, nil, s.audit).Create(ctx, kb.ID, KnowledgeAnomalyInput{
			KnowledgeIDs: []string{id}, Kind: "suspected_error", Description: "Historical strategy recheck: rejected: " + reason})
		return "flagged", err
	}
	current, err := s.knowledge.GetKnowledgeTags(ctx, []string{id})
	if err != nil {
		return "", err
	}
	var manual []types.KnowledgeTagManualDecision
	if err := s.db.WithContext(ctx).Where("knowledge_id = ?", id).Find(&manual).Error; err != nil {
		return "", err
	}
	predicted := make(map[string]bool)
	for _, label := range labels {
		tag, getErr := s.tags.GetByName(ctx, kb.TenantID, kb.ID, label.Name, label.Kind)
		if getErr != nil && !errors.Is(getErr, gorm.ErrRecordNotFound) {
			return "", getErr
		}
		if tag == nil {
			now := time.Now()
			tag = &types.KnowledgeTag{ID: uuid.NewString(), TenantID: kb.TenantID, KnowledgeBaseID: kb.ID,
				Kind: label.Kind, Name: label.Name, CreatedAt: now, UpdatedAt: now}
			if err := s.tags.Create(ctx, tag); err != nil {
				tag, getErr = s.tags.GetByName(ctx, kb.TenantID, kb.ID, label.Name, label.Kind)
				if getErr != nil {
					return "", getErr
				}
			}
		}
		predicted[tag.ID] = true
	}
	final := make(map[string]bool, len(predicted)+len(current[id]))
	for tagID := range predicted {
		final[tagID] = true
	}
	for _, tag := range current[id] {
		if !managedMessageTag(tag.Kind) {
			final[tag.ID] = true
		}
	}
	toAdd, toRemove := []string{}, []string{}
	for _, choice := range manual {
		if choice.Selected {
			final[choice.TagID] = true
			if !predicted[choice.TagID] {
				toRemove = append(toRemove, choice.TagID)
			}
		} else {
			delete(final, choice.TagID)
			if predicted[choice.TagID] {
				toAdd = append(toAdd, choice.TagID)
			}
		}
	}
	finalIDs := make([]string, 0, len(final))
	for tagID := range final {
		finalIDs = append(finalIDs, tagID)
	}
	unchanged := len(final) == len(current[id])
	if unchanged {
		for _, tag := range current[id] {
			if !final[tag.ID] {
				unchanged = false
				break
			}
		}
	}
	if len(toAdd)+len(toRemove) > 0 {
		_, err := NewKnowledgeAnomalyService(s.db, s.knowledge, nil, s.audit).Create(ctx, kb.ID, KnowledgeAnomalyInput{
			KnowledgeIDs: []string{id}, Kind: "manual_conflict", Description: "Historical strategy disagrees with manual tag corrections",
			ProposalType: "change_tags", ProposedTagIDs: toAdd, ProposedRemoveTagIDs: toRemove})
		if err != nil {
			return "", err
		}
	}
	if !unchanged {
		if err := s.knowledge.SetKnowledgeTags(ctx, id, finalIDs); err != nil {
			return "", err
		}
	}
	if len(toAdd)+len(toRemove) > 0 {
		if unchanged {
			return "conflict_unchanged", nil
		}
		return "conflict", nil
	}
	if unchanged {
		return "unchanged", nil
	}
	return "updated", nil
}

func managedMessageTag(kind string) bool {
	return kind == types.TagKindNature || kind == types.TagKindTopic || kind == types.TagKindObject || kind == types.TagKindEvent
}
