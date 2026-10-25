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

// EntityGraph derives directed edges from live facts without storing a graph replica.
func (s *EntityService) EntityGraph(
	ctx context.Context,
	id string,
	query types.EntityFactQuery,
) (*types.EntityGraph, error) {
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
	graph := &types.EntityGraph{Center: center, Nodes: []types.Entity{}, Edges: []types.EntityGraphEdge{}}
	err = q.Where("f.subject_entity_id=? OR f.object_entity_id=?", center.ID, center.ID).
		Joins(`JOIN entities subject ON subject.id=f.subject_entity_id
 AND subject.space_id=f.space_id AND subject.merged_into_id=''`).
		Joins(`JOIN entities object ON object.id=f.object_entity_id
 AND object.space_id=f.space_id AND object.merged_into_id=''`).
		Select(`DISTINCT ON (f.subject_entity_id,f.object_entity_id) f.subject_entity_id,f.object_entity_id,
 COUNT(*) OVER (PARTITION BY f.subject_entity_id,f.object_entity_id) AS fact_count, f.action AS latest_action`).
		Order("f.subject_entity_id,f.object_entity_id," + entityFactTime + " DESC,f.created_at DESC,f.id").
		Scan(&graph.Edges).Error
	if err != nil {
		return nil, err
	}
	ids := []string{center.ID}
	for _, edge := range graph.Edges {
		ids = appendUniqueEntityString(ids, edge.SubjectEntityID)
		ids = appendUniqueEntityString(ids, edge.ObjectEntityID)
	}
	err = s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, ids).
		Order("canonical_name,id").Find(&graph.Nodes).Error
	if err != nil {
		return nil, err
	}
	if err = s.loadAliases(ctx, spaceID, graph.Nodes); err != nil {
		return nil, err
	}
	for _, node := range graph.Nodes {
		if node.ID == center.ID {
			graph.Center = node
		}
	}
	return graph, nil
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
