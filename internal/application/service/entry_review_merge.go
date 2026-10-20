package service

import (
	"context"
	"encoding/json"
	"errors"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func documentReviewPrompt(library *types.Library) string {
	return intakeReviewPrompt(library) + `
你负责全文汇总审核，作出整份材料的最终收录判断。分段结果是待核对的判断，不是必须接受的结论。
先确定材料及其主要用途，再检查有无符合本库用途的实质信息和整体排除条件。完整性只作为使用限制。
有合格主体且没有成立的整体拒绝理由时通过。局部无关、宣传、寒暄或辅助内容不单独否决全文。不按合格段落比例打分，不要求每段有独立收藏价值，不采用“一段拒绝，全文拒绝”。
只有全文没有可辨认的实质信息，或材料的主要目的符合本库明确排除条件时才拒绝；正文截断、缺少步骤、代码、章节或附件不作为拒绝理由。
输入包含全部分段的 summary、判断、warning 及标题路径，不附带原文摘录或完整正文。依据分段摘要理解全文，区分主文的实质内容和附带内容；不能因为摘要未提到某项信息，就认定原文缺失。图片理解来自分段摘要，未向你提供的原图不能声称已亲自核对。
复核分段判断及其是否足以影响全文；摘要无法确认的疑点不升级为已确认缺失。保留仍然成立的分段 warning，并合并说明不妨碍收录的限制。
auxiliary 固定为 false。reason 说明整体通过或拒绝的依据，warning 说明保留的限制。只输出 approved、auxiliary、reason、warning 四个字段。`
}

func (s *entryService) mergeReviewSegments(ctx context.Context, library *types.Library, entry *types.Entry, body string, source types.SourceInfo, parts []segmentCheckpoint, model *types.Model, llm chat.Chat) (segmentReviewDecision, error) {
	segments := make([]any, 0, len(parts))
	runes := []rune(body)
	for _, part := range parts {
		decision, err := parseReviewDecision(string(part.Result), true, false)
		if err != nil {
			return segmentReviewDecision{}, err
		}
		position := part.ProcessingSegment
		position.ContextBefore = ""
		segments = append(segments, map[string]any{"segment": segmentInput(position, len(parts)), "review": decision})
	}
	prompt := documentReviewPrompt(library)
	input := map[string]any{"scope": "document", "title": articleTitleForProcessing(entry), "source_info": source, "segments": segments, "body_complete": false, "body_characters": len(runes)}
	encoded, err := json.Marshal(input)
	if err != nil {
		return segmentReviewDecision{}, err
	}
	_, _, completion := s.articleProcessingLimits(model)
	fits := func(encoded []byte) bool {
		return model.Parameters.ContextWindow <= 0 || len([]rune(string(encoded)))+len([]rune(prompt))+completion+modelOutputCorrectionReserve <= model.Parameters.ContextWindow
	}
	if !fits(encoded) {
		return segmentReviewDecision{}, errors.New("全文审核的分段摘要超过模型上下文，请提高审核模型上下文；未截断摘要或放行资料")
	}
	parent := s.tracker().LookupStage(ctx, entry.ID, attemptFromCtx(ctx), types.StageDocReader)
	span := s.tracker().BeginSubSpan(ctx, parent, "review.document", types.SpanKindGeneration, types.JSONMap{"segment_count": len(parts)})
	if span != nil {
		ctx = types.WithModelCallTrace(ctx, span.Attempt, span.SpanID)
	}
	var decision segmentReviewDecision
	err = chatWithOutputValidation(types.WithLLMCallMetadata(ctx, "intake_review", ""), llm,
		[]chat.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(encoded)}},
		&chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion}, func(raw string) error {
			var e error
			decision, e = parseReviewDecision(raw, false, true)
			return e
		})
	if err != nil {
		s.tracker().FailSpan(ctx, span, "DOCUMENT_REVIEW_FAILED", err.Error(), err)
		return decision, err
	}
	s.tracker().EndSpan(ctx, span, types.JSONMap{"segment_count": len(parts)})
	return decision, nil
}
