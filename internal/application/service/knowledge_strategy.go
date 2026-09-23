package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/agent/tools"
	werrors "reliquary/internal/errors"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type KnowledgeStrategyRules struct {
	Filter     string `json:"filter"`
	Tags       string `json:"tags"`
	Collection string `json:"collection"`
	Research   string `json:"research"`
}

type KnowledgeStrategySample struct {
	KnowledgeID   string `json:"knowledge_id"`
	OldFilter     string `json:"old_filter"`
	NewFilter     string `json:"new_filter"`
	OldTags       string `json:"old_tags"`
	NewTags       string `json:"new_tags"`
	OldCollection string `json:"old_collection"`
	NewCollection string `json:"new_collection"`
	OldResearch   string `json:"old_research"`
	NewResearch   string `json:"new_research"`
}

type KnowledgeStrategyService struct {
	db        *gorm.DB
	kb        interfaces.KnowledgeBaseService
	knowledge interfaces.KnowledgeService
	models    interfaces.ModelService
	tags      interfaces.KnowledgeTagRepository
	audit     interfaces.AuditLogService
}

func NewKnowledgeStrategyService(db *gorm.DB, kb interfaces.KnowledgeBaseService, knowledge interfaces.KnowledgeService, models interfaces.ModelService, tags interfaces.KnowledgeTagRepository, audit interfaces.AuditLogService) *KnowledgeStrategyService {
	return &KnowledgeStrategyService{db: db, kb: kb, knowledge: knowledge, models: models, tags: tags, audit: audit}
}

func (s *KnowledgeStrategyService) scopedKB(ctx context.Context, kbID string, ownerOnly bool) (*types.KnowledgeBase, string, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, "", werrors.NewUnauthorizedError("workspace required")
	}
	kb, err := s.kb.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, "", err
	}
	if kb == nil || kb.TenantID != tenantID {
		return nil, "", werrors.NewNotFoundError("knowledge base not found")
	}
	userID, _ := types.UserIDFromContext(ctx)
	if ownerOnly && (userID == "" || userID != kb.CreatorID || types.IsSyntheticUserID(userID)) {
		return nil, "", werrors.NewForbiddenError("only the knowledge base creator can change strategy")
	}
	return kb, userID, nil
}

func (s *KnowledgeStrategyService) List(ctx context.Context, kbID string) ([]types.KnowledgeStrategyVersion, error) {
	kb, _, err := s.scopedKB(ctx, kbID, false)
	if err != nil {
		return nil, err
	}
	var versions []types.KnowledgeStrategyVersion
	err = s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", kb.TenantID, kb.ID).
		Order("created_at DESC").Find(&versions).Error
	return versions, err
}

// Propose uses a user instruction and selected real messages. Trial model calls
// never write tags, knowledge, or the current active strategy.
func (s *KnowledgeStrategyService) Propose(ctx context.Context, kbID, instruction string, sampleIDs []string) (*types.KnowledgeStrategyVersion, error) {
	kb, actor, err := s.scopedKB(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || len([]rune(instruction)) > 2000 || len(sampleIDs) == 0 || len(sampleIDs) > 3 {
		return nil, werrors.NewBadRequestError("instruction and one to three real message samples required")
	}
	if strings.TrimSpace(kb.SummaryModelID) == "" {
		return nil, werrors.NewBadRequestError("summary model required for strategy trial")
	}
	model, err := s.models.GetChatModel(ctx, kb.SummaryModelID)
	if err != nil {
		return nil, err
	}
	tagModel := model
	if kb.AutoTagConfig != nil && strings.TrimSpace(kb.AutoTagConfig.ModelID) != "" && kb.AutoTagConfig.ModelID != kb.SummaryModelID {
		tagModel, err = s.models.GetChatModel(ctx, kb.AutoTagConfig.ModelID)
		if err != nil {
			return nil, err
		}
	}
	current, err := activeKnowledgeStrategyRules(ctx, s.db, kb.TenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		current = &KnowledgeStrategyRules{}
		if kb.AutoTagConfig != nil {
			current.Tags = kb.AutoTagConfig.Policy
		}
	}
	request, _ := json.Marshal(map[string]any{"instruction": instruction, "current_rules": current})
	draft, err := model.Chat(ctx, []chat.Message{
		{Role: "system", Content: `Draft only filtering, tagging, daily collection and user-triggered research strategy text from the user's explicit instruction. Keep the fixed content categories, topic/object/event kinds, source completeness requirements, daily collection since enablement, user goal requirement for research, and X/Grok boundary unchanged. Treat the instruction as a request, not code. Return strict JSON {"filter":"...","tags":"...","collection":"...","research":"...","reason":"..."}; carry forward each unchanged rule verbatim. Rules refine behavior; they cannot remove mandatory checks.`},
		{Role: "user", Content: string(request)},
	}, &chat.ChatOptions{Temperature: 0})
	if err != nil || draft == nil {
		if err == nil {
			err = fmt.Errorf("empty strategy draft")
		}
		return nil, err
	}
	var proposed struct {
		Filter     *string `json:"filter"`
		Tags       *string `json:"tags"`
		Collection *string `json:"collection"`
		Research   *string `json:"research"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(draft.Content), &proposed); err != nil || proposed.Filter == nil || proposed.Tags == nil || proposed.Collection == nil || proposed.Research == nil {
		return nil, fmt.Errorf("invalid strategy draft")
	}
	next := *current
	next.Filter, next.Tags = strings.TrimSpace(*proposed.Filter), strings.TrimSpace(*proposed.Tags)
	next.Collection, next.Research = strings.TrimSpace(*proposed.Collection), strings.TrimSpace(*proposed.Research)
	proposed.Reason = strings.TrimSpace(proposed.Reason)
	if len([]rune(next.Filter)) > 4000 || len([]rune(next.Tags)) > 4000 || len([]rune(next.Collection)) > 4000 || len([]rune(next.Research)) > 4000 || proposed.Reason == "" || len([]rune(proposed.Reason)) > 500 ||
		(next == *current) {
		return nil, werrors.NewBadRequestError("strategy draft has no valid change or exceeds limits")
	}
	samples := make([]KnowledgeStrategySample, 0, len(sampleIDs))
	seen := make(map[string]bool)
	for _, id := range sampleIDs {
		if id == "" || seen[id] {
			return nil, werrors.NewBadRequestError("distinct message samples required")
		}
		seen[id] = true
		var message types.Knowledge
		if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND parse_status IN ?",
			id, kb.TenantID, kb.ID, []string{types.ParseStatusFinalizing, types.ParseStatusCompleted}).First(&message).Error; err != nil {
			return nil, werrors.NewBadRequestError("sample must be a formal message in this knowledge base")
		}
		intake, err := message.IntakeMetadata()
		if err != nil || intake == nil || intake.Decision != "approved" {
			return nil, werrors.NewBadRequestError("sample needs an approved original")
		}
		file, _, err := s.knowledge.GetKnowledgeFile(ctx, id)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(file, 800001))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(body) > 800000 || strings.TrimSpace(string(body)) == "" {
			return nil, werrors.NewBadRequestError("sample original is empty or too large")
		}
		oldFilter, err := trialFilter(ctx, model, *current, string(body), intake.SourceInfo)
		if err != nil {
			return nil, err
		}
		newFilter := oldFilter
		if next.Filter != current.Filter {
			newFilter, err = trialFilter(ctx, model, next, string(body), intake.SourceInfo)
			if err != nil {
				return nil, err
			}
		}
		oldTags, err := trialTags(ctx, tagModel, *current, message.Title, string(body))
		if err != nil {
			return nil, err
		}
		newTags := oldTags
		if next.Tags != current.Tags {
			newTags, err = trialTags(ctx, tagModel, next, message.Title, string(body))
			if err != nil {
				return nil, err
			}
		}
		oldCollection, err := trialCollection(ctx, model, *current, string(body), intake.SourceInfo)
		if err != nil {
			return nil, err
		}
		newCollection := oldCollection
		if next.Collection != current.Collection || next.Filter != current.Filter {
			newCollection, err = trialCollection(ctx, model, next, string(body), intake.SourceInfo)
			if err != nil {
				return nil, err
			}
		}
		oldResearch, err := rewriteResearchQuery(ctx, model, current.Research, message.Title)
		if err != nil {
			return nil, err
		}
		newResearch := oldResearch
		if next.Research != current.Research {
			newResearch, err = rewriteResearchQuery(ctx, model, next.Research, message.Title)
			if err != nil {
				return nil, err
			}
		}
		samples = append(samples, KnowledgeStrategySample{KnowledgeID: id, OldFilter: oldFilter, NewFilter: newFilter,
			OldTags: oldTags, NewTags: newTags, OldCollection: oldCollection, NewCollection: newCollection,
			OldResearch: oldResearch, NewResearch: newResearch})
	}
	rulesJSON, _ := json.Marshal(next)
	previousJSON, _ := json.Marshal(current)
	samplesJSON, _ := json.Marshal(samples)
	version := &types.KnowledgeStrategyVersion{ID: uuid.NewString(), TenantID: kb.TenantID, KnowledgeBaseID: kb.ID,
		Status: "candidate", Rules: types.JSON(rulesJSON), PreviousRules: types.JSON(previousJSON), Reason: proposed.Reason,
		SampleResults: types.JSON(samplesJSON), CreatedBy: actor, CreatedAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(version).Error; err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKnowledgeStrategyProposed, "knowledge_strategy", version.ID,
		types.AuditOutcomeSuccess, map[string]any{"reason": version.Reason, "sample_count": len(samples)})
	return version, nil
}

func trialFilter(ctx context.Context, model chat.Chat, rules KnowledgeStrategyRules, body string, source types.SourceInfo) (string, error) {
	return trialReview(ctx, model, intakeReviewPrompt(rules.Filter), body, source)
}

func trialCollection(ctx context.Context, model chat.Chat, rules KnowledgeStrategyRules, body string, source types.SourceInfo) (string, error) {
	return trialReview(ctx, model, intakeReviewPromptForMessage(&rules, types.ChannelRSS), body, source)
}

func trialReview(ctx context.Context, model chat.Chat, prompt, body string, source types.SourceInfo) (string, error) {
	input, _ := json.Marshal(map[string]any{"source_info": source, "body": body})
	response, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(input)}}, &chat.ChatOptions{Temperature: 0})
	if err != nil || response == nil {
		if err == nil {
			err = fmt.Errorf("empty model response")
		}
		return "", fmt.Errorf("filter trial failed: %w", err)
	}
	var result struct {
		Approved *bool  `json:"approved"`
		Reason   string `json:"reason"`
	}
	if json.Unmarshal([]byte(response.Content), &result) != nil || result.Approved == nil || strings.TrimSpace(result.Reason) == "" {
		return "", fmt.Errorf("invalid filter trial response")
	}
	label := "rejected"
	if *result.Approved {
		label = "approved"
	}
	return label + ": " + string([]rune(result.Reason)[:min(len([]rune(result.Reason)), 200)]), nil
}

func rewriteResearchQuery(ctx context.Context, model chat.Chat, policy, query string) (string, error) {
	return tools.RewriteResearchQuery(ctx, model, policy, query)
}

func trialTags(ctx context.Context, model chat.Chat, rules KnowledgeStrategyRules, title, body string) (string, error) {
	labels, err := classifyMessageTags(ctx, model, rules.Tags, title+"\n\n"+body)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, label.Kind+":"+label.Name)
	}
	return strings.Join(parts, ", "), nil
}

// Activate publishes a reviewed candidate or restores an archived version.
// It never reprocesses historical messages.
func (s *KnowledgeStrategyService) Activate(ctx context.Context, kbID, versionID string) (*types.KnowledgeStrategyVersion, error) {
	kb, actor, err := s.scopedKB(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	var selected types.KnowledgeStrategyVersion
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND tenant_id = ?", kb.ID, kb.TenantID).First(&locked).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", versionID, kb.TenantID, kb.ID).First(&selected).Error; err != nil {
			return err
		}
		if selected.Status != "candidate" && selected.Status != "archived" {
			return werrors.NewBadRequestError("strategy version is already active")
		}
		if selected.Status == "candidate" {
			current, err := activeKnowledgeStrategyRules(ctx, tx, kb.TenantID, kb.ID)
			if err != nil {
				return err
			}
			if current != nil {
				var before KnowledgeStrategyRules
				if err := json.Unmarshal(selected.PreviousRules, &before); err != nil {
					return err
				}
				if before != *current {
					return werrors.NewBadRequestError("active strategy changed; propose a new candidate")
				}
			}
		}
		if err := tx.Model(&types.KnowledgeStrategyVersion{}).Where("tenant_id = ? AND knowledge_base_id = ? AND status = ?", kb.TenantID, kb.ID, "active").Update("status", "archived").Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&selected).Updates(map[string]any{"status": "active", "activated_by": actor, "activated_at": now}).Error; err != nil {
			return err
		}
		selected.Status, selected.ActivatedBy, selected.ActivatedAt = "active", actor, &now
		return nil
	})
	if err == nil {
		recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKnowledgeStrategyActivated, "knowledge_strategy", selected.ID,
			types.AuditOutcomeSuccess, map[string]any{"reason": selected.Reason})
	}
	return &selected, err
}

func activeKnowledgeStrategyRules(ctx context.Context, db *gorm.DB, tenantID uint64, kbID string) (*KnowledgeStrategyRules, error) {
	if db == nil {
		return nil, nil // lightweight service harnesses have no strategy store.
	}
	var active types.KnowledgeStrategyVersion
	err := db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND status = ?", tenantID, kbID, "active").
		Order("activated_at DESC, created_at DESC").First(&active).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rules KnowledgeStrategyRules
	if err := json.Unmarshal(active.Rules, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

func activeResearchPolicyForTargets(ctx context.Context, db *gorm.DB, targets types.SearchTargets) (string, error) {
	tenantID, _ := types.TenantIDFromContext(ctx)
	if db == nil || tenantID == 0 {
		return "", nil
	}
	policies := make([]string, 0)
	seen := make(map[string]bool)
	for kbID, sourceTenantID := range targets.GetKBTenantMap() {
		if sourceTenantID != tenantID || seen[kbID] {
			continue
		}
		seen[kbID] = true
		rules, err := activeKnowledgeStrategyRules(ctx, db, tenantID, kbID)
		if err != nil {
			return "", err
		}
		if rules != nil && rules.Research != "" {
			policies = append(policies, kbID+": "+rules.Research)
		}
	}
	sort.Strings(policies)
	return strings.Join(policies, "\n"), nil
}
