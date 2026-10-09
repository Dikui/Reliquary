package service

import (
	"context"

	"reliquary/internal/types"
)

// A resumed workflow may retain old query results. Recount coverage when the
// report is generated without changing its frozen facts or source citations.
func (s *KnowledgeWorkflowService) refreshReportEventCoverage(ctx context.Context, wf *types.KnowledgeWorkflow, material *workflowReportMaterial) error {
	steps := map[string]types.KnowledgeWorkflowStep{}
	for _, step := range wf.Steps {
		steps[step.ID] = step
	}
	for index := range material.EventCoverage {
		step, exists := steps[material.EventCoverage[index].StepID]
		if !exists || step.Type != types.WorkflowStepFactQuery {
			continue
		}
		cfg, err := decodeWorkflowConfig[workflowFactConfig](step.Config)
		if err != nil {
			return err
		}
		q := types.KnowledgeContextQuery{Scope: cfg.Scope, DomainID: cfg.DomainID, KnowledgeBaseID: cfg.KnowledgeBaseID, Entity: cfg.Entity, EntityID: cfg.EntityID, ObjectEntityID: cfg.ObjectEntityID}
		q.Since, err = parseWorkflowTime(cfg.Since)
		if err != nil {
			return err
		}
		q.Until, err = parseWorkflowTime(cfg.Until)
		if err != nil {
			return err
		}
		scope, err := s.context.resolveContextQueryScope(ctx, wf.KnowledgeBaseID, q)
		if err != nil {
			return err
		}
		_, entityIDs, _, err := s.context.contextQueryCandidates(ctx, scope, q)
		if err != nil {
			return err
		}
		count, err := s.context.templateOnlyMessageCount(ctx, scope, q, entityIDs)
		if err != nil {
			return err
		}
		material.EventCoverage[index].TemplateOnlyMessageCount = count
	}
	return nil
}

// A search-only report has no structured query scope. Count its cited messages
// against their latest snapshots instead of treating old citations as current.
func (s *KnowledgeWorkflowService) reportSourceTemplateOnlyCount(ctx context.Context, citations []types.KnowledgeWorkflowCitation) (int, error) {
	var ids []string
	for _, citation := range citations {
		ids = append(ids, citation.KnowledgeID)
	}
	ids = uniqueContextIDs(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	var sources []types.KnowledgeEvidence
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_id IN ? AND status = ? AND extraction_mode = ?", tenantID, ids, types.KnowledgeEvidenceCurrent, types.KnowledgeExtractionTemplateOnly).
		Where("version = (SELECT MAX(newer.version) FROM knowledge_context_evidence newer WHERE newer.tenant_id = knowledge_context_evidence.tenant_id AND newer.knowledge_id = knowledge_context_evidence.knowledge_id)").Find(&sources).Error; err != nil {
		return 0, err
	}
	sources, err := s.context.visibleContextEvidence(ctx, tenantID, "", sources, false)
	if err != nil {
		return 0, err
	}
	messages := map[string]bool{}
	for _, source := range sources {
		messages[source.KnowledgeID] = true
	}
	return len(messages), nil
}
