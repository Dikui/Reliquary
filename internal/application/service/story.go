package service

import (
	"context"
	"errors"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"strings"

	"gorm.io/gorm"
)

// StoryService reads fact timelines and applies deliberate grouping corrections.
type StoryService struct{ db *gorm.DB }

// NewStoryService constructs the story timeline and correction service.
func NewStoryService(db *gorm.DB) *StoryService { return &StoryService{db: db} }

func (s *StoryService) visibleFacts(ctx context.Context, spaceID uint64) *gorm.DB {
	return s.db.WithContext(ctx).Table("facts f").Where("f.space_id=? AND f.merged_into_id=''", spaceID).
		Where(`(f.manual_corrected OR EXISTS
 (SELECT 1 FROM entry_facts ef JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id
 WHERE ef.space_id=f.space_id AND ef.fact_id=f.id AND ef.role='source' AND e.deleted_at IS NULL))`)
}

// ListStories orders by the arrival of the most recent new fact, independently of its occurrence date.
func (s *StoryService) ListStories(ctx context.Context, query types.StoryQuery) (*types.StoryPage, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	for name, ids := range map[string][]string{"domain_ids": query.DomainIDs, "entity_ids": query.EntityIDs} {
		if len(ids) > 100 {
			return nil, werrors.NewBadRequestError(name + " accepts at most 100 IDs")
		}
		for _, id := range ids {
			if strings.TrimSpace(id) == "" {
				return nil, werrors.NewBadRequestError(name + " cannot contain an empty ID")
			}
		}
	}
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 20
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	page := &types.StoryPage{Items: []types.Story{}}
	visible := s.visibleFacts(ctx, spaceID).Select("f.story_id")
	q := s.db.WithContext(ctx).Model(&types.Story{}).Where("space_id=? AND id IN (?)", spaceID, visible)
	if len(query.EntityIDs) > 0 {
		entities := s.visibleFacts(ctx, spaceID).Select("f.story_id").
			Where("f.subject_entity_id IN ? OR f.object_entity_id IN ?", query.EntityIDs, query.EntityIDs)
		q = q.Where("id IN (?)", entities)
	}
	if len(query.DomainIDs) > 0 {
		domains := s.visibleFacts(ctx, spaceID).Select("f.story_id").
			Joins("JOIN entry_facts ef ON ef.space_id=f.space_id AND ef.fact_id=f.id AND ef.role='source'").
			Joins("JOIN entries e ON e.space_id=ef.space_id AND e.id=ef.entry_id AND e.deleted_at IS NULL").
			Joins("JOIN entry_domains ed ON ed.space_id=e.space_id AND ed.entry_id=e.id").
			Where("ed.domain_id IN ?", query.DomainIDs)
		q = q.Where("id IN (?)", domains)
	}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	err = q.Order("last_fact_at DESC, id").Limit(query.Limit).Offset(query.Offset).Find(&page.Items).Error
	return page, err
}

// StoryDetail returns the chronological timeline with each source and its verbatim quotation.
func (s *StoryService) StoryDetail(ctx context.Context, id string) (*types.StoryDetail, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	detail := &types.StoryDetail{
		Facts:          []types.StoryFact{},
		Mentions:       []types.FactSource{},
		RelatedStories: []types.Story{},
	}
	detail.Story, err = canonicalStory(s.db.WithContext(ctx), spaceID, id)
	if err != nil {
		return nil, err
	}
	id = detail.Story.ID
	facts := []types.Fact{}
	err = s.visibleFacts(ctx, spaceID).Select("f.*").Where("f.story_id=?", id).
		Order(`COALESCE(CASE WHEN length(f.occurred_at)=10
 THEN (f.occurred_at || 'T00:00:00Z')::timestamptz
 ELSE f.occurred_at::timestamptz END, f.created_at), f.created_at, f.id`).Scan(&facts).Error
	if err != nil {
		return nil, err
	}
	detail.Facts, err = factsWithSources(ctx, s.db, spaceID, facts)
	if err != nil {
		return nil, err
	}

	err = s.db.WithContext(ctx).Table("entry_facts ef").
		Select(`DISTINCT ON (ef.entry_id) ef.entry_id,e.library_id,e.title,ef.role,ef.quote,ef.manual,
   COALESCE(e.metadata#>>'{intake,source_info,original_url}',e.metadata->>'original_url','') AS original_url`).
		Joins("JOIN facts f ON f.id=ef.fact_id AND f.space_id=ef.space_id").
		Joins("JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id AND e.deleted_at IS NULL").
		Where("f.space_id=? AND f.story_id=? AND ef.role='mention'", spaceID, id).
		Where("f.id IN (?)", s.visibleFacts(ctx, spaceID).Select("f.id")).
		Order("ef.entry_id,ef.created_at").Scan(&detail.Mentions).Error
	if err != nil {
		return nil, err
	}
	// Only direct neighbours are returned. Recount live evidence on every read so
	// revisions and deletions take effect immediately, independently of the hourly sweep.
	err = s.db.WithContext(ctx).Table("story_links links").Select("related.*").
		Joins(`JOIN stories related ON related.space_id=links.space_id
 AND related.id=CASE WHEN links.story_id=? THEN links.other_story_id ELSE links.story_id END`, id).
		Where("links.space_id=? AND (links.story_id=? OR links.other_story_id=?)", spaceID, id, id).
		Where("related.merged_into_id=''").
		Where(`(links.story_id,links.other_story_id) IN (?)`, bridgePairs(s.db.WithContext(ctx), spaceID, 2)).
		Order("related.last_fact_at DESC,related.id").Scan(&detail.RelatedStories).Error
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// GroupingDecisions exposes the retained candidate comparisons for one material.
func (s *StoryService) GroupingDecisions(ctx context.Context, entryID string) ([]types.GroupingDecision, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	var entry types.Entry
	if err = s.db.WithContext(ctx).Select("id").
		Where("space_id=? AND id=?", spaceID, entryID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("entry not found")
		}
		return nil, err
	}
	decisions := []types.GroupingDecision{}
	err = s.db.WithContext(ctx).
		Where("space_id=? AND entry_id=?", spaceID, entryID).
		Order("created_at,id").
		Find(&decisions).
		Error
	return decisions, err
}
