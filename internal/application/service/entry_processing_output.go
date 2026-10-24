package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	catalog, _ := json.Marshal(l.ContentTypes)
	prompt := articleProcessingPrompt() + "\n所属库：" + l.Name + "\ncontent_types=" + string(catalog) + `
先从 content_types 的 enabled=true 项选择 content_type，拿不准或清单外选其他。有其他时使用“其他”，否则使用 other。`
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
	if k.ContentTypeCorrected {
		name, _ := json.Marshal(k.ContentType)
		prompt += "\n用户已经修正 content_type=" + string(name) + "，必须使用该类型，不再重新分类。"
	}
	if !l.ExtractsEntities() {
		return prompt + "\n本库不抽取实体或实体短介绍。entities 必须返回空数组。"
	}
	return prompt + "\n" + articleEntityProcessingPrompt()
}

func libraryContentType(l *types.Library, value string) string {
	value = strings.TrimSpace(value)
	for _, row := range l.ContentTypes {
		if row.Enabled && row.Name == value {
			return value
		}
	}
	for _, row := range l.ContentTypes {
		if row.Enabled && row.Name == "其他" {
			return "其他"
		}
	}
	return types.ContentTypeOther
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

func eligibleProcessingEntity(entity articleProcessingEntity) bool {
	if entity.Type == nil || (entity.Relevance != "central" && entity.Relevance != "participant") {
		return false
	}
	switch types.NormalizeEntityType(*entity.Type) {
	case "company", "product", "organization":
		return true
	}
	return false
}

func qualifySegmentOutput(l *types.Library, k *types.Entry, body string, p types.ProcessingSegment, result *articleProcessingResult) {
	if !l.ExtractsEntities() {
		result.Entities = []articleProcessingEntity{}
	}
	ct := libraryContentType(l, *result.ContentType)
	if k.ContentTypeCorrected {
		ct = libraryContentType(l, k.ContentType)
	}
	result.ContentType = &ct
	runes := []rune(body)
	_, blocks := articleEvidenceBody(string(runes[p.Start:p.End]))
	accepted := []articleProcessingEntity{}
	for _, entity := range result.Entities {
		if !eligibleProcessingEntity(entity) {
			continue
		}
		entity.Aliases = nil
		entity.Description = strings.TrimSpace(entity.Description)
		locations := articleEvidenceLocations(entity.EvidenceIDs, blocks)
		if len(locations) == 0 {
			continue
		}
		entity.Evidence = []types.SourceLocation{}
		for _, location := range locations {
			entity.Evidence = append(entity.Evidence, bodyRangeLocations(body, p.Start+location.Start, p.Start+location.End, "exact")...)
		}
		accepted = append(accepted, entity)
	}
	result.Entities = accepted
}

// ReadEntryProcessingOutput supplies the latest accepted processing evidence.
// Scope and saved-body hash let consumers reject stale evidence after supplementation.
func ReadEntryProcessingOutput(ctx context.Context, db *gorm.DB, spaceID uint64, entryID string) (*types.EntryProcessingOutput, error) {
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
		EntryID:     k.ID,
		SpaceID:     k.SpaceID,
		LibraryID:   k.LibraryID,
		RunID:       result.RunID,
		BodyHash:    fmt.Sprintf("%x", sha256.Sum256([]byte(body))),
		ContentType: k.ContentType,
		Images:      result.Images,
		UpdatedAt:   time.Now(),
	}
	if out.Images == nil {
		out.Images = []types.ProcessingImage{}
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}}, UpdateAll: true}).Create(out).Error
}
