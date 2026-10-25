package service

import (
	"context"
	"encoding/json"
	"reliquary/internal/types"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
)

func (s *entryService) validateFactMentions(
	ctx context.Context,
	library *types.Library,
	request *types.IngestRequest,
) error {
	if request.MentionedFactIDs == nil {
		return nil
	}
	if library.Template != types.TemplateGenerated {
		return werrors.NewBadRequestError("mentioned_fact_ids is only available for AI outputs")
	}
	ids := []string{}
	for _, id := range *request.MentionedFactIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return werrors.NewBadRequestError("mentioned_fact_ids contains an empty ID")
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	var count int64
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Model(&types.Fact{}).
			Where("space_id=? AND id IN ?", library.SpaceID, ids).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(ids)) {
			return werrors.NewBadRequestError("mentioned_fact_ids contains an unknown fact")
		}
	}
	request.MentionedFactIDs = &ids
	return nil
}

func setRequestedFactMentions(entry *types.Entry, request *types.IngestRequest) error {
	if request.MentionedFactIDs == nil {
		return nil
	}
	metadata := map[string]json.RawMessage{}
	if len(entry.Metadata) > 0 {
		if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(*request.MentionedFactIDs)
	if err != nil {
		return err
	}
	metadata["mentioned_fact_ids"] = raw
	entry.Metadata, err = json.Marshal(metadata)
	return err
}

func hasExplicitFactMentions(entry *types.Entry) bool {
	var metadata map[string]json.RawMessage
	_ = json.Unmarshal(entry.Metadata, &metadata)
	_, ok := metadata["mentioned_fact_ids"]
	return ok
}

func sameRequestedFactMentions(a, b *types.Entry) bool {
	var left, right map[string]json.RawMessage
	_ = json.Unmarshal(a.Metadata, &left)
	_ = json.Unmarshal(b.Metadata, &right)
	return string(left["mentioned_fact_ids"]) == string(right["mentioned_fact_ids"])
}

func (s *entryService) saveExplicitFactMentions(ctx context.Context, entry *types.Entry, ids []string) error {
	if err := s.db.WithContext(ctx).
		Where("space_id=? AND entry_id=? AND role='mention' AND manual=false", entry.SpaceID, entry.ID).
		Delete(&types.EntryFact{}).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&types.EntryFact{
			SpaceID: entry.SpaceID, EntryID: entry.ID, FactID: id, Role: "mention", CreatedAt: time.Now(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
