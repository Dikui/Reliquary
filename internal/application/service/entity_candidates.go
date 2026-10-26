package service

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"time"

	werrors "reliquary/internal/errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func candidateQuery(tx *gorm.DB, spaceID uint64) *gorm.DB {
	return tx.Model(&types.EntityCandidate{}).Where("entity_candidates.space_id=?", spaceID)
}

func candidateCountSelect() string {
	return `entity_candidates.*, (SELECT count(*) FROM entity_candidate_occurrences o
 WHERE o.space_id=entity_candidates.space_id AND o.candidate_id=entity_candidates.id) +
 (SELECT count(*) FROM entity_candidate_entries e
 WHERE e.space_id=entity_candidates.space_id AND e.candidate_id=entity_candidates.id) AS occurrence_count`
}

// ListCandidates orders reviewed names by their recorded endpoint count.
func (s *EntityService) ListCandidates(
	ctx context.Context,
	status string,
	limit, offset int,
) (*types.EntityCandidatePage, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	query := candidateQuery(s.db.WithContext(ctx), spaceID)
	if status != "all" {
		switch status {
		case "pending", "ignored", "approved", "aliased":
		default:
			return nil, werrors.NewBadRequestError("invalid candidate status")
		}
		query = query.Where("status=?", status)
	}
	page := &types.EntityCandidatePage{Items: []types.EntityCandidate{}}
	if err := query.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	err = query.Select(candidateCountSelect()).
		Order("occurrence_count DESC,entity_candidates.created_at,entity_candidates.id").
		Limit(limit).
		Offset(offset).
		Find(&page.Items).
		Error
	return page, err
}

func findCandidate(tx *gorm.DB, spaceID uint64, id string) (*types.EntityCandidate, error) {
	var item types.EntityCandidate
	err := candidateQuery(
		tx,
		spaceID,
	).Select(candidateCountSelect()).
		Where("entity_candidates.id=?", id).
		First(&item).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, werrors.NewNotFoundError("entity candidate not found")
	}
	return &item, err
}

// CandidateDetail returns one bounded page across fact endpoints and material evidence.
func (s *EntityService) CandidateDetail(
	ctx context.Context,
	id string,
	limit, offset int,
) (*types.EntityCandidateDetail, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	item, err := findCandidate(s.db.WithContext(ctx), spaceID, id)
	if err != nil {
		return nil, err
	}
	result := &types.EntityCandidateDetail{
		Candidate:   *item,
		Occurrences: []types.EntityCandidateFact{},
		Total:       item.OccurrenceCount,
	}
	var occurrences []struct {
		FactID    string
		EntryID   string
		Title     string
		Endpoint  string
		Quote     string
		CreatedAt time.Time
	}
	err = s.db.WithContext(ctx).Raw(`SELECT * FROM (
 SELECT fact_id, '' AS entry_id, '' AS title, endpoint, '' AS quote, created_at
 FROM entity_candidate_occurrences WHERE space_id=? AND candidate_id=?
 UNION ALL
 SELECT '' AS fact_id, ce.entry_id, e.title, '' AS endpoint, ce.quote, ce.created_at
 FROM entity_candidate_entries ce JOIN entries e ON e.space_id=ce.space_id AND e.id=ce.entry_id
 WHERE ce.space_id=? AND ce.candidate_id=?
 ) sources ORDER BY created_at,fact_id,entry_id,endpoint LIMIT ? OFFSET ?`,
		spaceID, id, spaceID, id, limit, offset).Scan(&occurrences).Error
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, occurrence := range occurrences {
		if occurrence.FactID != "" {
			ids = append(ids, occurrence.FactID)
		}
	}
	facts := []types.Fact{}
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, ids).Find(&facts).Error; err != nil {
			return nil, err
		}
	}
	byID := map[string]types.Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}
	for _, occurrence := range occurrences {
		source := types.EntityCandidateFact{CreatedAt: occurrence.CreatedAt}
		if occurrence.FactID != "" {
			fact := byID[occurrence.FactID]
			source.Fact, source.Endpoint = &fact, occurrence.Endpoint
		} else {
			source.Entry = &types.EntityCandidateEntryInfo{ID: occurrence.EntryID, Title: occurrence.Title}
			source.Quote = occurrence.Quote
		}
		result.Occurrences = append(result.Occurrences, source)
	}
	return result, nil
}

// ReviewCandidate serializes review with directory edits and fact creation so
// an ignored name can keep accumulating occurrences without losing a decision.
func (s *EntityService) ReviewCandidate(
	ctx context.Context,
	id, action, targetID string,
) (*types.EntityCandidate, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated entity editor required")
	}
	var result *types.EntityCandidate
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		item, err := findCandidate(tx, spaceID, id)
		if err != nil {
			return err
		}
		result = item
		before := *item
		if item.Status == "approved" || item.Status == "aliased" {
			if (action == "approve" && item.Status == "approved") ||
				(action == "alias" && item.Status == "aliased" && item.EntityID != nil && *item.EntityID == targetID) {
				return nil
			}
			return werrors.NewBadRequestError("entity candidate already resolved")
		}
		switch action {
		case "ignore":
			if item.Status == "ignored" {
				return nil
			}
			item.Status = "ignored"
		case "approve":
			input, err := validateEntityInput(types.EntityInput{CanonicalName: item.Name})
			if err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&types.Entity{}).
				Where("space_id=? AND normalized_name=? AND merged_into_id=''",
					spaceID, item.NormalizedName).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return werrors.NewBadRequestError("entity name already exists; merge the candidate as an alias")
			}
			now := time.Now()
			entity := types.Entity{
				ID: uuid.NewString(), SpaceID: spaceID, CanonicalName: input.CanonicalName,
				NormalizedName: item.NormalizedName, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
			item.Status, item.EntityID = "approved", &entity.ID
		case "alias":
			var entity types.Entity
			err := tx.Where("space_id=? AND id=? AND merged_into_id=''", spaceID, targetID).First(&entity).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("entity not found")
			}
			if err != nil {
				return err
			}
			if err := saveEntityAlias(tx, entity, item.Name, "manual"); err != nil {
				return err
			}
			item.Status, item.EntityID = "aliased", &entity.ID
		default:
			return werrors.NewBadRequestError("invalid candidate action")
		}
		item.UpdatedAt = time.Now()
		if err := tx.Model(item).Updates(map[string]any{
			"status": item.Status, "entity_id": item.EntityID, "updated_at": item.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		if item.EntityID != nil {
			for _, endpoint := range []string{"subject", "object"} {
				if err := tx.Model(&types.Fact{}).Where(`space_id=? AND id IN
 (SELECT fact_id FROM entity_candidate_occurrences WHERE space_id=? AND candidate_id=? AND endpoint=?)`,
					spaceID, spaceID, item.ID, endpoint).
					Update(endpoint+"_entity_id", *item.EntityID).Error; err != nil {
					return err
				}
			}
			if err := attachCandidateEntries(tx, *item); err != nil {
				return err
			}
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, action+"_entity_candidate", item.ID, before, item)
	})
	return result, err
}
