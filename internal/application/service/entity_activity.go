package service

import (
	"context"
	"errors"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"time"

	"gorm.io/gorm"
)

// Unknown occurrence dates use arrival time, as in the story timeline.
const entityFactTime = `COALESCE(CASE WHEN length(f.occurred_at)=10
 THEN (f.occurred_at || 'T00:00:00Z')::timestamptz
 ELSE f.occurred_at::timestamptz END, f.created_at)`

func canonicalEntity(db *gorm.DB, spaceID uint64, id string) (types.Entity, error) {
	visited := map[string]bool{}
	for !visited[id] {
		visited[id] = true
		var entity types.Entity
		if err := db.Where("space_id=? AND id=?", spaceID, id).First(&entity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return entity, werrors.NewNotFoundError("entity not found")
			}
			return entity, err
		}
		if entity.MergedIntoID == "" {
			return entity, nil
		}
		id = entity.MergedIntoID
	}
	return types.Entity{}, werrors.NewBadRequestError("entity merge cycle")
}

func entityTimeRange(q *gorm.DB, from, to string) (*gorm.DB, error) {
	parse := func(value string) (time.Time, error) {
		if len(value) == 10 {
			return time.Parse(time.DateOnly, value)
		}
		return time.Parse(time.RFC3339Nano, value)
	}
	var start, end time.Time
	var err error
	if from != "" {
		start, err = parse(from)
		if err != nil {
			return nil, werrors.NewBadRequestError("from must be YYYY-MM-DD or RFC3339")
		}
		q = q.Where(entityFactTime+" >= ?", start)
	}
	if to != "" {
		end, err = parse(to)
		if err != nil {
			return nil, werrors.NewBadRequestError("to must be YYYY-MM-DD or RFC3339")
		}
		if len(to) == 10 {
			end = end.AddDate(0, 0, 1)
			q = q.Where(entityFactTime+" < ?", end)
		} else {
			q = q.Where(entityFactTime+" <= ?", end)
		}
		if from != "" && (start.After(end) || (len(to) == 10 && start.Equal(end))) {
			return nil, werrors.NewBadRequestError("from must not be after to")
		}
	}
	return q, nil
}

// EntityFacts reads all visible endpoint facts, including single-ended occurrences.
func (s *EntityService) EntityFacts(
	ctx context.Context,
	id string,
	query types.EntityFactQuery,
) (*types.EntityFactPage, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	center, err := canonicalEntity(s.db.WithContext(ctx), spaceID, id)
	if err != nil {
		return nil, err
	}
	q, err := entityTimeRange(NewStoryService(s.db).visibleFacts(ctx, spaceID), query.From, query.To)
	if err != nil {
		return nil, err
	}
	q = q.Where("f.subject_entity_id=? OR f.object_entity_id=?", center.ID, center.ID)
	if query.OtherEntityID != "" {
		other, err := canonicalEntity(s.db.WithContext(ctx), spaceID, query.OtherEntityID)
		if err != nil {
			return nil, err
		}
		q = q.Where(`(f.subject_entity_id=? AND f.object_entity_id=?) OR
 (f.subject_entity_id=? AND f.object_entity_id=?)`, center.ID, other.ID, other.ID, center.ID)
	}
	switch query.Direction {
	case "outgoing":
		q = q.Where("f.subject_entity_id=?", center.ID)
	case "incoming":
		q = q.Where("f.object_entity_id=?", center.ID)
	case "":
	default:
		return nil, werrors.NewBadRequestError("direction must be incoming or outgoing")
	}
	page := &types.EntityFactPage{Items: []types.StoryFact{}}
	if err = q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	facts := []types.Fact{}
	err = q.Select("f.*").Order(entityFactTime + ",f.created_at,f.id").
		Limit(entityQueryLimit(query.Limit)).Offset(max(query.Offset, 0)).Scan(&facts).Error
	if err != nil {
		return nil, err
	}
	page.Items, err = factsWithSources(ctx, s.db, spaceID, facts)
	return page, err
}
