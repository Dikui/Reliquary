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

type KnowledgeStrategyEvaluationSample struct {
	KnowledgeID string `json:"knowledge_id"`
	Label       string `json:"label"`
	Split       string `json:"split"`
}

type KnowledgeStrategyConfusion struct {
	TP        int      `json:"tp"`
	FP        int      `json:"fp"`
	FN        int      `json:"fn"`
	TN        int      `json:"tn"`
	Precision *float64 `json:"precision"`
	Recall    *float64 `json:"recall"`
}

type KnowledgeStrategyEvaluationCase struct {
	KnowledgeID string `json:"knowledge_id"`
	Label       string `json:"label"`
	Split       string `json:"split"`
	OldDecision string `json:"old_decision"`
	NewDecision string `json:"new_decision"`
	OldReason   string `json:"old_reason"`
	NewReason   string `json:"new_reason"`
	OldCorrect  *bool  `json:"old_correct"`
	NewCorrect  *bool  `json:"new_correct"`
}

type KnowledgeStrategyEvaluationSplit struct {
	Old     KnowledgeStrategyConfusion `json:"old"`
	New     KnowledgeStrategyConfusion `json:"new"`
	Samples int                        `json:"samples"`
}

type KnowledgeStrategyEvaluation struct {
	VersionID   string                            `json:"version_id"`
	Development KnowledgeStrategyEvaluationSplit  `json:"development"`
	Holdout     KnowledgeStrategyEvaluationSplit  `json:"holdout"`
	Cases       []KnowledgeStrategyEvaluationCase `json:"cases"`
}

type KnowledgeStrategyService struct {
	db        *gorm.DB
	kb        interfaces.KnowledgeBaseService
	knowledge interfaces.KnowledgeService
	models    interfaces.ModelService
	tags      interfaces.KnowledgeTagRepository
}

func NewKnowledgeStrategyService(db *gorm.DB, kb interfaces.KnowledgeBaseService, knowledge interfaces.KnowledgeService, models interfaces.ModelService, tags interfaces.KnowledgeTagRepository) *KnowledgeStrategyService {
	return &KnowledgeStrategyService{db: db, kb: kb, knowledge: knowledge, models: models, tags: tags}
}

func (s *KnowledgeStrategyService) scopedKB(ctx context.Context, kbID string, requireEditor bool) (*types.KnowledgeBase, string, error) {
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
	if requireEditor && (userID == "" || !types.IsAuthenticatedFromContext(ctx)) {
		return nil, "", werrors.NewForbiddenError("an authenticated user is required to change strategy")
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
	model, err := GetTierChatModel(ctx, s.db, s.models, kb, types.ModelTierLight)
	if err != nil {
		return nil, err
	}
	tagModel := model
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
		file, _, err := s.knowledge.GetKnowledgeBody(ctx, id)
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
	return version, nil
}

// Evaluate runs labeled originals through the frozen previous and candidate
// filter rules. It does not persist labels, decisions, or strategy changes.
func (s *KnowledgeStrategyService) Evaluate(ctx context.Context, kbID, versionID string, samples []KnowledgeStrategyEvaluationSample) (*KnowledgeStrategyEvaluation, error) {
	kb, _, err := s.scopedKB(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	if len(samples) == 0 || len(samples) > 20 {
		return nil, werrors.NewBadRequestError("one to twenty labeled samples required")
	}
	samples = append([]KnowledgeStrategyEvaluationSample(nil), samples...)
	var version types.KnowledgeStrategyVersion
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", versionID, kb.TenantID, kb.ID).First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("strategy version not found")
		}
		return nil, err
	}
	var oldRules, newRules KnowledgeStrategyRules
	previousRules := version.PreviousRules
	if len(previousRules) == 0 {
		previousRules = types.JSON(`{}`)
	}
	if err := json.Unmarshal(previousRules, &oldRules); err != nil {
		return nil, fmt.Errorf("invalid previous strategy rules: %w", err)
	}
	if err := json.Unmarshal(version.Rules, &newRules); err != nil {
		return nil, fmt.Errorf("invalid candidate strategy rules: %w", err)
	}

	seen := make(map[string]bool, len(samples))
	messages := make([]types.Knowledge, 0, len(samples))
	bodies := make([]string, 0, len(samples))
	for i := range samples {
		sample := &samples[i]
		sample.KnowledgeID = strings.TrimSpace(sample.KnowledgeID)
		if sample.KnowledgeID == "" || seen[sample.KnowledgeID] ||
			(sample.Label != "select" && sample.Label != "reject" && sample.Label != "either") ||
			(sample.Split != "development" && sample.Split != "holdout") {
			return nil, werrors.NewBadRequestError("samples need distinct IDs, a select/reject/either label, and a development/holdout split")
		}
		seen[sample.KnowledgeID] = true
		var message types.Knowledge
		if err := s.db.WithContext(ctx).Select("id", "tenant_id", "knowledge_base_id", "title", "channel", "parse_status", "metadata", "deleted_at").
			Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", sample.KnowledgeID, kb.TenantID, kb.ID).First(&message).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, werrors.NewBadRequestError("sample must belong to this knowledge base and have an original")
			}
			return nil, err
		}
		if message.IsSourceUpdateCandidate() || !strategyEvaluationStatusAllowed(message.ParseStatus) {
			return nil, werrors.NewBadRequestError("sample must be a formal, pending, or rejected source in this knowledge base")
		}
		intake, err := message.IntakeMetadata()
		if err != nil {
			return nil, werrors.NewBadRequestError("sample intake metadata is invalid")
		}
		if intake != nil && intake.Decision != "approved" && intake.Decision != "pending" && intake.Decision != "rejected" {
			return nil, werrors.NewBadRequestError("sample must be a formal, pending, or rejected source")
		}
		switch message.ParseStatus {
		case types.ParseStatusPending:
			if intake == nil || intake.Decision != "pending" {
				return nil, werrors.NewBadRequestError("pending sample original is unavailable")
			}
		case types.ParseStatusRejected:
			if intake == nil || intake.Decision != "rejected" {
				return nil, werrors.NewBadRequestError("rejected sample original is unavailable")
			}
		case types.ParseStatusFinalizing, types.ParseStatusCompleted:
			if intake != nil && intake.Decision != "approved" {
				return nil, werrors.NewBadRequestError("formal sample must have an approved original")
			}
		}
		file, _, err := s.knowledge.GetKnowledgeBody(ctx, message.ID)
		if err != nil || file == nil {
			if file != nil {
				_ = file.Close()
			}
			return nil, werrors.NewBadRequestError("sample original is unavailable")
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
		messages = append(messages, message)
		bodies = append(bodies, string(body))
	}

	model, err := GetTierChatModel(ctx, s.db, s.models, kb, types.ModelTierLight)
	if err != nil {
		return nil, err
	}
	result := &KnowledgeStrategyEvaluation{VersionID: version.ID, Cases: make([]KnowledgeStrategyEvaluationCase, 0, len(samples))}
	for i, sample := range samples {
		message := messages[i]
		var source types.SourceInfo
		if intake, _ := message.IntakeMetadata(); intake != nil {
			source = intake.SourceInfo
		}
		oldDecision, oldReason, err := strategyFilterDecision(ctx, model, oldRules, message.Channel, bodies[i], source)
		if err != nil {
			return nil, err
		}
		newDecision, newReason, err := strategyFilterDecision(ctx, model, newRules, message.Channel, bodies[i], source)
		if err != nil {
			return nil, err
		}
		item := KnowledgeStrategyEvaluationCase{KnowledgeID: sample.KnowledgeID, Label: sample.Label, Split: sample.Split,
			OldDecision: oldDecision, NewDecision: newDecision, OldReason: oldReason, NewReason: newReason}
		if sample.Label != "either" {
			oldCorrect := (sample.Label == "select") == (oldDecision == "approved")
			newCorrect := (sample.Label == "select") == (newDecision == "approved")
			item.OldCorrect, item.NewCorrect = &oldCorrect, &newCorrect
		}
		result.Cases = append(result.Cases, item)
		target := &result.Development
		if sample.Split == "holdout" {
			target = &result.Holdout
		}
		if sample.Label != "either" {
			target.Samples++
			addStrategyConfusion(&target.Old, sample.Label, oldDecision)
			addStrategyConfusion(&target.New, sample.Label, newDecision)
		}
	}
	setStrategyMetrics(&result.Development.Old)
	setStrategyMetrics(&result.Development.New)
	setStrategyMetrics(&result.Holdout.Old)
	setStrategyMetrics(&result.Holdout.New)
	return result, nil
}

func strategyEvaluationStatusAllowed(status string) bool {
	switch status {
	case types.ParseStatusPending, types.ParseStatusFinalizing, types.ParseStatusCompleted, types.ParseStatusRejected:
		return true
	default:
		return false
	}
}

func strategyFilterDecision(ctx context.Context, model chat.Chat, rules KnowledgeStrategyRules, channel, body string, source types.SourceInfo) (string, string, error) {
	filterOnly := KnowledgeStrategyRules{Filter: rules.Filter}
	result, err := trialReview(ctx, model, intakeReviewPromptForMessage(&filterOnly, channel), body, source)
	if err != nil {
		return "", "", err
	}
	decision, reason, ok := strings.Cut(result, ": ")
	if !ok || (decision != "approved" && decision != "rejected") {
		return "", "", fmt.Errorf("invalid filter trial decision")
	}
	return decision, reason, nil
}

func addStrategyConfusion(result *KnowledgeStrategyConfusion, label, decision string) {
	selected := decision == "approved"
	switch {
	case label == "select" && selected:
		result.TP++
	case label == "reject" && selected:
		result.FP++
	case label == "select" && !selected:
		result.FN++
	case label == "reject" && !selected:
		result.TN++
	}
}

func setStrategyMetrics(result *KnowledgeStrategyConfusion) {
	if denominator := result.TP + result.FP; denominator > 0 {
		value := float64(result.TP) / float64(denominator)
		result.Precision = &value
	}
	if denominator := result.TP + result.FN; denominator > 0 {
		value := float64(result.TP) / float64(denominator)
		result.Recall = &value
	}
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
