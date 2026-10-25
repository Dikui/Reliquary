package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reliquary/internal/config"
	"reliquary/internal/types"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type extractedImage struct {
	Index       int    `json:"index"`
	Description string `json:"description"`
}

func libraryProcessingPrompt(l *types.Library, k *types.Entry) string {
	prompt := articleProcessingPrompt() + "\n所属库：" + l.Name
	switch {
	case l.SystemRole == "media":
		prompt += "\n" + config.Prompt("library_media")
	case l.Template == types.TemplateExternalDurable:
		prompt += "\n" + config.Prompt("library_reference")
	case l.Template == types.TemplateOwn:
		prompt += "\n" + config.Prompt("library_own")
	default:
		prompt += "\n" + config.Prompt("library_institutional")
	}
	if materialCanProduceFact(l, k) {
		prompt += `
仅当本篇报道一次具体发生时，额外输出一个 fact 对象：{"title":"事实标题","subject":"主体","action":"本次动作","object":"对象","conditions":"条件",
"occurred_at":"RFC3339 或 YYYY-MM-DD，未知用空字符串","quote":"一句逐字原文",
"subject_entity_id":"已有实体 ID 或空字符串","object_entity_id":"已有实体 ID 或空字符串",
"subject_type":"organization / person / other","object_type":"organization / person / other"}。
端点 ID 只能来自 entity_options；没有 ID 时标明类型，产品和模型用 other。署名作者不作为事实端点提取。
综合稿、评论、教程不输出事实，fact 为 null。提取本次动作而非背景动作，一篇资料最多一个事实。
quote 必须逐字出自正文，不含段落编号，不翻译、不改写、不拼接。`
	} else {
		prompt += "\n本资料不得产生事实，fact 必须为 null。"
	}
	return prompt
}

func bodyRangeLocations(body string, start, end int, mapping string) []types.SourceLocation {
	r := []rune(body)
	if start < 0 || end <= start || end > len(r) {
		return []types.SourceLocation{}
	}
	c := &types.Chunk{Content: string(r[start:end]), StartAt: start, EndAt: end}
	types.AssignSourceLocations(body, []*types.Chunk{c})
	for i := range c.SourceLocations {
		c.SourceLocations[i].Mapping = mapping
	}
	return c.SourceLocations
}

// Number existing Markdown blocks without changing stored text or coordinates.
func articleEvidenceBody(body string) (string, []types.SourceLocation) {
	runes := []rune(body)
	locations := bodyRangeLocations(body, 0, len(runes), "exact")
	var numbered strings.Builder
	for i, location := range locations {
		fmt.Fprintf(&numbered, "[P%d]\n%s\n", i+1, string(runes[location.Start:location.End]))
	}
	return numbered.String(), locations
}

func articleEvidenceLocations(ids []int, blocks []types.SourceLocation) []types.SourceLocation {
	if len(ids) == 0 {
		return nil
	}
	locations := []types.SourceLocation{}
	seen := map[int]bool{}
	for _, id := range ids {
		if id < 1 || id > len(blocks) {
			return nil
		}
		if !seen[id] {
			locations = append(locations, blocks[id-1])
			seen[id] = true
		}
	}
	return locations
}

func qualifySegmentOutput(
	library *types.Library,
	entry *types.Entry,
	body string,
	_ types.ProcessingSegment,
	result *articleProcessingResult,
) {
	result.Fact = qualifyUnderstandingFact(library, entry, body, result.Fact)
	// Ignore legacy segment payloads containing extracted names.
	result.Entities = nil
	result.ResolvedEntities = nil
}

// ReadEntryProcessingOutput supplies the latest accepted processing evidence.
// Scope and saved-body hash let consumers reject stale evidence after supplementation.
func ReadEntryProcessingOutput(
	ctx context.Context,
	db *gorm.DB,
	spaceID uint64,
	entryID string,
) (*types.EntryProcessingOutput, error) {
	var result types.EntryProcessingOutput
	err := db.WithContext(ctx).Where("space_id=? AND entry_id=?", spaceID, entryID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (s *entryService) saveProcessingOutput(
	ctx context.Context,
	k *types.Entry,
	body string,
	result *articleProcessingResult,
) error {
	out := &types.EntryProcessingOutput{
		EntryID:   k.ID,
		SpaceID:   k.SpaceID,
		LibraryID: k.LibraryID,
		RunID:     result.RunID,
		BodyHash:  fmt.Sprintf("%x", sha256.Sum256([]byte(body))),
		Images:    result.Images,
		UpdatedAt: time.Now(),
	}
	if out.Images == nil {
		out.Images = []types.ProcessingImage{}
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}}, UpdateAll: true}).Create(out).Error
}
