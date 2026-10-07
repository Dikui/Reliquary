package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *KnowledgeContextService) CorrectFact(ctx context.Context, kbID, factID string, correction types.KnowledgeFactCorrection) (*types.KnowledgeFact, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	if kbID != "" {
		if _, err := s.scopedKnowledgeBase(ctx, kbID); err != nil {
			return nil, err
		}
	}
	actor, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return nil, werrors.NewUnauthorizedError("authenticated correction actor required")
	}
	correction.Quote = strings.TrimSpace(correction.Quote)
	correction.CorrectionNote = strings.TrimSpace(correction.CorrectionNote)
	if correction.Quote == "" || correction.CorrectionNote == "" {
		return nil, werrors.NewBadRequestError("a source quote and correction note are required")
	}
	var corrected *types.KnowledgeFact
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContextTeam(tx, tenantID); err != nil {
			return err
		}
		var old types.KnowledgeFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", factID, tenantID).First(&old).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("knowledge fact not found")
			}
			return err
		}
		if err := types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, old.KnowledgeBaseID); err != nil {
			return err
		}
		if kbID != "" && old.KnowledgeBaseID != kbID {
			return werrors.NewNotFoundError("knowledge fact not found")
		}
		kbID = old.KnowledgeBaseID
		if old.Kind != types.KnowledgeFactAttribute {
			return werrors.NewBadRequestError("only attributed facts can be corrected through this operation")
		}
		if old.Status != types.KnowledgeFactCurrent {
			return werrors.NewBadRequestError("only a current fact can be corrected")
		}
		var evidence types.KnowledgeEvidence
		if err := tx.Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", old.EvidenceID, tenantID, kbID).First(&evidence).Error; err != nil {
			return err
		}
		if !verbatimQuote(evidence.Body, correction.Quote) {
			return werrors.NewBadRequestError("correction quote must be present in its source evidence")
		}
		value := correction.Value
		if len(value) == 0 {
			return werrors.NewBadRequestError("a corrected value is required")
		}
		correction.ValueType = strings.TrimSpace(correction.ValueType)
		if !validContextValue(json.RawMessage(value), correction.ValueType) {
			return werrors.NewBadRequestError("corrected value does not match its declared type")
		}
		from, to := correction.EffectiveFrom, correction.EffectiveTo
		if from != nil && to != nil && to.Before(*from) {
			return werrors.NewBadRequestError("corrected effective range is reversed")
		}
		newFact := types.KnowledgeFact{
			ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID,
			SubjectEntityID: old.SubjectEntityID, Kind: old.Kind, Predicate: old.Predicate, OriginalPredicate: old.OriginalPredicate,
			Value: value, ValueType: correction.ValueType, Unit: correction.Unit,
			EffectiveFrom: from, EffectiveTo: to, EvidenceID: old.EvidenceID,
			Quote: correction.Quote, Status: types.KnowledgeFactCurrent,
			SupersedesID: old.ID, CorrectionNote: correction.CorrectionNote,
			CorrectionActor: actor.StorageID(), CreatedAt: time.Now(),
		}
		if err := tx.Create(&newFact).Error; err != nil {
			return err
		}
		previous := old
		if err := tx.Model(&old).Updates(map[string]any{"status": types.KnowledgeFactCorrected, "corrected_by": newFact.ID}).Error; err != nil {
			return err
		}
		if err := writeContextAudit(tx, ctx, tenantID, "knowledge_context.fact_corrected", "fact", newFact.ID, map[string]any{"knowledge_base_id": kbID, "previous_fact": previous, "corrected_fact": newFact}); err != nil {
			return err
		}
		corrected = &newFact
		return nil
	})
	return corrected, err
}

func (s *KnowledgeContextService) MergeEntities(ctx context.Context, kbID, keepEntityID, mergeEntityID string) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	if key, ok := types.TenantAPIKeyScopeFromContext(ctx); ok && key.IsKnowledgeBaseRestricted() {
		return werrors.NewForbiddenError("global object merges require an unrestricted team API key")
	}
	if kbID != "" {
		if _, err := s.scopedKnowledgeBase(ctx, kbID); err != nil {
			return err
		}
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return werrors.NewUnauthorizedError("authenticated merge actor required")
	}
	if keepEntityID == "" || mergeEntityID == "" || keepEntityID == mergeEntityID {
		return werrors.NewBadRequestError("two distinct entity IDs are required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContextTeam(tx, tenantID); err != nil {
			return err
		}
		var keep, merged types.KnowledgeEntity
		targets := []struct {
			id     string
			target *types.KnowledgeEntity
		}{{keepEntityID, &keep}, {mergeEntityID, &merged}}
		sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })
		for _, item := range targets {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", item.id, tenantID).First(item.target).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return werrors.NewNotFoundError("knowledge entity not found")
				}
				return err
			}
		}
		if merged.MergedIntoID == keep.ID {
			return nil
		}
		if keep.MergedIntoID != "" || merged.MergedIntoID != "" {
			return werrors.NewBadRequestError("entity was already merged into a different object")
		}
		var previousFacts []types.KnowledgeFact
		var previousLinks []types.KnowledgeEventEvidence
		var previousEvents []types.KnowledgeEvent
		if err := tx.Where("tenant_id = ? AND (subject_entity_id = ? OR object_entity_id = ?)", tenantID, merged.ID, merged.ID).Find(&previousFacts).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ?", tenantID).Find(&previousLinks).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ?", tenantID).Find(&previousEvents).Error; err != nil {
			return err
		}
		filteredLinks := previousLinks[:0]
		for _, link := range previousLinks {
			if containsString(link.EntityIDs, merged.ID) {
				filteredLinks = append(filteredLinks, link)
			}
		}
		previousLinks = filteredLinks
		filteredEvents := previousEvents[:0]
		for _, event := range previousEvents {
			if containsString(event.EntityIDs, merged.ID) {
				filteredEvents = append(filteredEvents, event)
			}
		}
		previousEvents = filteredEvents
		var aliases []types.KnowledgeEntityAlias
		if err := tx.Where("tenant_id = ? AND entity_id = ?", tenantID, merged.ID).Find(&aliases).Error; err != nil {
			return err
		}
		for _, alias := range aliases {
			var collision types.KnowledgeEntityAlias
			err := tx.Where("tenant_id = ? AND entity_type = ? AND normalized_alias = ? AND entity_id <> ?", tenantID, keep.EntityType, alias.NormalizedAlias, merged.ID).First(&collision).Error
			if err == nil {
				if collision.EntityID != keep.ID {
					return werrors.NewBadRequestError("merge alias conflicts with another team object")
				}
				if err := tx.Delete(&alias).Error; err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Model(&alias).Updates(map[string]any{"entity_id": keep.ID, "entity_type": keep.EntityType}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&types.KnowledgeFact{}).Where("tenant_id = ? AND subject_entity_id = ?", tenantID, merged.ID).Update("subject_entity_id", keep.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.KnowledgeFact{}).Where("tenant_id = ? AND object_entity_id = ?", tenantID, merged.ID).Update("object_entity_id", keep.ID).Error; err != nil {
			return err
		}
		var eventLinks []types.KnowledgeEventEvidence
		if err := tx.Where("tenant_id = ?", tenantID).Find(&eventLinks).Error; err != nil {
			return err
		}
		for _, link := range eventLinks {
			if !containsString(link.EntityIDs, merged.ID) {
				continue
			}
			link.EntityIDs = appendUniqueContextString(link.EntityIDs, keep.ID)
			out := types.StringArray{}
			for _, id := range link.EntityIDs {
				if id != merged.ID {
					out = appendUniqueContextString(out, id)
				}
			}
			if err := tx.Model(&link).Update("entity_ids", out).Error; err != nil {
				return err
			}
		}
		var events []types.KnowledgeEvent
		if err := tx.Where("tenant_id = ?", tenantID).Find(&events).Error; err != nil {
			return err
		}
		for _, event := range events {
			if !containsString(event.EntityIDs, merged.ID) {
				continue
			}
			event.EntityIDs = appendUniqueContextString(event.EntityIDs, keep.ID)
			out := types.StringArray{}
			for _, id := range event.EntityIDs {
				if id != merged.ID {
					out = appendUniqueContextString(out, id)
				}
			}
			if err := tx.Model(&event).Update("entity_ids", out).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&merged).Update("merged_into_id", keep.ID).Error; err != nil {
			return err
		}
		return writeContextAudit(tx, ctx, tenantID, "knowledge_context.entities_merged", "entity", keep.ID, map[string]any{"scope": "team", "kept_entity": keep, "merged_entity": merged, "aliases_before": aliases, "facts_before": previousFacts, "event_links_before": previousLinks, "events_before": previousEvents})
	})
}

// The audit row is committed in the same transaction as the global change.
func writeContextAudit(tx *gorm.DB, ctx context.Context, tenantID uint64, action types.AuditAction, targetType, targetID string, details any) error {
	actor, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return werrors.NewUnauthorizedError("authenticated context editor required")
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return tx.Create(&types.AuditLog{TenantID: tenantID, ActorUserID: actor.ID, ActorRole: string(types.TenantRoleFromContext(ctx)), Action: action, ScopeType: "team", ScopeID: fmt.Sprint(tenantID), TargetType: targetType, TargetID: targetID, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(encoded), CreatedAt: time.Now()}).Error
}
