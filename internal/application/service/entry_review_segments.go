package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type segmentReviewDecision struct {
	Approved  *bool  `json:"approved"`
	Auxiliary bool   `json:"auxiliary,omitempty"`
	Reason    string `json:"reason"`
	Warning   string `json:"warning,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type reviewImageInput struct {
	Index      int    `json:"index"`
	Start      int    `json:"start"`
	Provided   bool   `json:"provided"`
	SkipReason string `json:"skip_reason,omitempty"`
}

type deferSegmentReviewKey struct{}

var errSegmentReviewQueued = errors.New("segmented intake review requires background execution")

func segmentReviewPrompt(library *types.Library) string {
	return intakeReviewPrompt(library) + `
你看到的是一个分段，未必是独立文章。利用 segment 的编号、总段数、标题路径和前文理解它的作用。不要要求每段重复作者、背景、引言或结论，不因分段边界判断缺失。
A. 本段提供符合本库用途的实质信息：approved=true，auxiliary=false。附带的宣传、寒暄或无关信息不影响其中合格内容的成立。
B. 本段仅有辅助内容，或局部内容不值得收录但不能证明全文不合格：approved=false，auxiliary=true。例如导航、参考文献、页脚、局部邀请、重复内容或需要后续上下文的片段。此判定不否决全文。
C. 现有证据足以证明整份材料违反本库明确排除条件：approved=false，auxiliary=false。必须说明证据为何适用于全文，不得把局部不相关、宣传或暂缺上下文升级为全文拒绝。
只有一个分段时，它就是全部材料，可以判断整体有无实质内容。有多个分段时，最终结论由全文汇总审核作出。
images 给出图片索引、在本段正文中的 Unicode 字符位置、是否提供及未读取原因。附件顺序对应 provided=true 的记录，保留原索引。summary 中明确区分文字信息和实际读到的图片信息。
多个分段时，JSON 另须提供 summary：用不超过 600 字概括本段主文的实质内容、引用与上下文的作用以及重要限制，供全文汇总审核使用。保留具体事实及必要条件，不得只重复通过或拒绝结论，不要求逐字摘录原文。纯辅助段也要概括作用。
有缺失疑点但证据不足时，说明限制，不把疑点写成已确认的缺失。`
}

func (s *entryService) reviewImageCount(model *types.Model, body string) int {
	if !model.Parameters.SupportsVision {
		return 0
	}
	_, limit, _ := s.articleProcessingLimits(model)
	count := 0
	for _, ref := range bodyImageReferences(body) {
		if !remoteIngestImage(ref.URL) {
			count++
		}
	}
	return min(limit, count)
}

func (s *entryService) rejectSegmentLimit(ctx context.Context, entry *types.Entry, err error) (bool, error) {
	var limit *types.SegmentLimitError
	if !errors.As(err, &limit) {
		return false, err
	}
	intake, e := entry.IntakeMetadata()
	if e != nil {
		return false, e
	}
	if intake == nil {
		intake = &types.EntryIntakeMetadata{}
	}
	intake.Decision, intake.Reason = "rejected", limit.Error()
	entry.ParseStatus, entry.EnableStatus = types.ParseStatusRejected, "disabled"
	if e = entry.SetIntakeMetadata(intake); e != nil {
		return false, e
	}
	return true, s.repo.UpdateEntry(ctx, entry)
}

func (s *entryService) hasSegmentCheckpoint(ctx context.Context, entry *types.Entry) bool {
	if s.db == nil {
		return false
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&entrySegmentRun{}).Where("entry_id=? AND space_id=? AND status IN ?", entry.ID, entry.SpaceID, []string{"pending", "running", "failed"}).Count(&count).Error
	// A database outage must not turn a resumable failure into source deletion.
	return err != nil || count > 0
}

func (s *entryService) singleReviewSegment(ctx context.Context, library *types.Library, entry *types.Entry, body string) bool {
	id, err := ResolveModelTier(ctx, s.db, library, types.ModelTierLight)
	if err != nil || id == "" || s.modelService == nil {
		return false
	}
	model, err := s.modelService.GetModelByID(ctx, id)
	if err != nil {
		return false
	}
	intake, err := entry.IntakeMetadata()
	if err != nil || intake == nil {
		return false
	}
	source := intake.SourceInfo
	source.Confirmed = false
	metadata := struct {
		SourceInfo types.SourceInfo `json:"source_info"`
	}{source}
	budget, err := s.segmentBudget(model, segmentReviewPrompt(library), metadata, s.reviewImageCount(model, body))
	if err != nil {
		return false
	}
	_, imageLimit, _ := s.articleProcessingLimits(model)
	return len([]rune(body)) <= budget && len(bodyImageReferences(body)) <= imageLimit
}

func (s *entryService) reviewSegments(ctx context.Context, library *types.Library, entry *types.Entry, body string, source types.SourceInfo) (bool, string, error) {
	if err := s.rejectProcessingGIFs(ctx, library, entry, body); err != nil {
		return false, "", err
	}
	ctx = types.WithModelCallEntry(ctx, entry.ID)
	id, err := ResolveModelTier(ctx, s.db, library, types.ModelTierLight)
	if err != nil {
		return false, "", err
	}
	if id == "" {
		return false, "", errors.New("light model is not configured")
	}
	if s.modelService == nil {
		return false, "", errors.New("model service is unavailable")
	}
	model, err := s.modelService.GetModelByID(ctx, id)
	if err != nil {
		return false, "", err
	}
	if model == nil {
		return false, "", errors.New("light model is unavailable")
	}
	prompt := segmentReviewPrompt(library)
	metadata := struct {
		SourceInfo types.SourceInfo `json:"source_info"`
	}{source}
	budget, err := s.segmentBudget(model, prompt, metadata, s.reviewImageCount(model, body))
	if err != nil {
		return false, "", err
	}
	// Enforce the processing cap before spending any review calls as well.
	if generationID, e := ResolveModelTier(ctx, s.db, library, types.ModelTierGeneration); e == nil && generationID != "" {
		input, processingPrompt, generation, e := s.prepareArticleProcessing(ctx, library, entry)
		if e != nil {
			return false, "", e
		}
		_, images, _ := s.articleProcessingLimits(generation)
		imageCount := 0
		if generation.Parameters.SupportsVision {
			imageCount = min(images, len(articleImagesInOrder(body, nil)))
		}
		processingBudget, e := s.segmentBudget(generation, processingPrompt, input, imageCount)
		if e != nil {
			return false, "", e
		}
		limit, overlap, _ := s.segmentSettings()
		if _, e = types.PlanProcessingSegments(body, processingBudget, overlap, limit); e != nil {
			return false, "", e
		}
	}
	run, err := s.openSegmentRun(ctx, entry, "review", body, budget, []any{prompt, metadata, articleTitleForProcessing(entry), documentReviewPrompt(library), segmentModelIdentity(model)})
	if err != nil {
		return false, "", err
	}
	if deferReview, _ := ctx.Value(deferSegmentReviewKey{}).(bool); deferReview {
		var plan []segmentCheckpoint
		if err := json.Unmarshal(run.Plan, &plan); err != nil {
			return false, "", err
		}
		if len(plan) > 1 {
			return false, "", errSegmentReviewQueued
		}
	}
	llm, err := GetTierChatModel(ctx, s.db, s.modelService, library, types.ModelTierLight)
	if err != nil {
		return false, "", err
	}
	runes := []rune(body)
	parts, err := s.executeSegments(ctx, entry, run, func(ctx context.Context, p types.ProcessingSegment, count int) (json.RawMessage, error) {
		partBody := string(runes[p.Start:p.End])
		refs := segmentImageReferences(body, p)
		images, _, records, err := s.attachProcessingImages(ctx, library, entry, partBody, model.Parameters.SupportsVision, refs)
		if err != nil {
			return nil, err
		}
		imageInputs := make([]reviewImageInput, 0, len(records))
		imageReferences := []types.ModelCallImageReference{}
		for i, record := range records {
			imageInputs = append(imageInputs, reviewImageInput{Index: i + 1, Start: len([]rune(partBody[:refs[i].Position])), Provided: record.SkipReason == "", SkipReason: record.SkipReason})
			if record.SkipReason == "" {
				_, payload, _ := strings.Cut(images[len(imageReferences)], ",")
				data, _ := base64.StdEncoding.DecodeString(payload)
				imageReferences = append(imageReferences, types.ModelCallImageReference{Path: record.URL, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))})
			}
		}
		ctx = types.WithModelCallImages(ctx, imageReferences)
		input := map[string]any{"source_info": source, "body": partBody, "segment": segmentInput(p, count)}
		if len(imageInputs) > 0 {
			input["images"] = imageInputs
		}
		encoded, e := json.Marshal(input)
		if e != nil {
			return nil, e
		}
		_, _, completion := s.articleProcessingLimits(model)
		if model.Parameters.ContextWindow > 0 && len([]rune(string(encoded)))+len([]rune(prompt))+completion+len(images)*1024 > model.Parameters.ContextWindow {
			return nil, errors.New("review segment exceeds model context budget")
		}
		user := chat.Message{Role: "user", Content: string(encoded), Images: images}
		var decision segmentReviewDecision
		e = chatWithOutputValidation(types.WithLLMCallMetadata(ctx, "intake_review", ""), llm, []chat.Message{{Role: "system", Content: prompt}, user}, &chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion}, func(raw string) error {
			var err error
			decision, err = parseReviewDecision(raw, count > 1, false)
			return err
		})
		if e != nil {
			return nil, e
		}
		return json.Marshal(decision)
	})
	if err != nil {
		return false, "", err
	}
	var decision segmentReviewDecision
	err = s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error {
		if len(current.Result) > 0 {
			return json.Unmarshal(current.Result, &decision)
		}
		var e error
		if len(parts) > 1 {
			decision, e = s.mergeReviewSegments(ctx, library, entry, body, source, parts, model, llm)
		} else {
			decision, e = parseReviewDecision(string(parts[0].Result), false, false)
			if e == nil && decision.Auxiliary {
				decision.Reason = "全文缺少符合收录标准的实质正文：" + decision.Reason
			} else if e == nil && !*decision.Approved {
				decision.Reason = "第 1/1 段: " + decision.Reason
			}
		}
		if e != nil {
			return e
		}
		raw, e := json.Marshal(decision)
		current.Result, current.Status = types.JSON(raw), "completed"
		return e
	})
	if err != nil {
		if ctx.Err() == nil {
			_ = s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error {
				if len(current.Result) == 0 {
					current.Status = "failed"
				}
				return nil
			})
		}
		return false, "", err
	}
	reason := decision.Reason
	if *decision.Approved {
		reason = decision.Warning
	}
	return *decision.Approved, reason, nil
}

func parseReviewDecision(raw string, segmented, document bool) (segmentReviewDecision, error) {
	var decision segmentReviewDecision
	if err := utils.ParseLLMJsonResponse(raw, &decision); err != nil {
		return decision, fmt.Errorf("invalid intake review response: %w", err)
	}
	if decision.Approved == nil {
		return decision, errors.New("invalid intake review response: approved must be a boolean")
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return decision, errors.New("invalid intake review response: reason must be non-empty")
	}
	if decision.Auxiliary && (*decision.Approved || document) {
		return decision, errors.New("invalid intake review response: auxiliary is only allowed for rejected segments")
	}
	if segmented && (strings.TrimSpace(decision.Summary) == "" || len([]rune(decision.Summary)) > 600) {
		return decision, errors.New("invalid intake review response: each segment requires a summary of at most 600 characters")
	}
	return decision, nil
}
