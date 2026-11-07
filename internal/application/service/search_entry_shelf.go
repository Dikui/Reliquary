package service

import (
	"context"
	"slices"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Resolve only explicit, currently effective shelf structure. In particular,
// following a part never walks back into the other models that use it.
func (s *searchService) shelfSearchOwners(ctx context.Context, spaceID uint64, libraries, roots []string) ([]string, error) {
	var found []string
	err := s.db.WithContext(ctx).Table("entities e").
		Joins("JOIN libraries l ON l.id=e.library_id AND l.space_id=e.space_id").
		Where(`e.space_id=? AND e.library_id IN ? AND e.id IN ? AND e.archived_at IS NULL AND e.merged_into_id=''
			AND e.kind IN ('product_line','platform','product','model','part')
			AND l.deleted_at IS NULL AND l.entity_mode='managed' AND l.shelf_enabled`, spaceID, libraries, roots).
		Pluck("e.id", &found).Error
	if err != nil {
		return nil, err
	}
	for _, id := range roots {
		if !slices.Contains(found, id) {
			return nil, apperrors.NewBadRequestError("shelf_node_ids must identify active shelf nodes in the selected libraries")
		}
	}
	at := time.Now()
	var owners []string
	err = s.db.WithContext(ctx).Raw(`WITH RECURSIVE edges AS MATERIALIZED (
		SELECT r.* FROM relations r WHERE r.space_id=? AND r.library_id IN ?
		AND r.valid_from<=? AND (r.valid_to IS NULL OR r.valid_to>?)
	), descendants(id,library_id) AS (
		SELECT e.id,e.library_id FROM entities e WHERE e.space_id=? AND e.id IN ? AND e.library_id IN ?
		UNION
		SELECT e.id,e.library_id FROM descendants d JOIN edges r ON r.object_id=d.id AND r.library_id=d.library_id
		JOIN entities e ON e.id=r.subject_id AND e.library_id=d.library_id AND e.space_id=r.space_id
		WHERE r.relation_type='belongs_to' AND e.archived_at IS NULL AND e.merged_into_id=''
	), owners(id,library_id) AS (
		SELECT id,library_id FROM descendants
		UNION
		SELECT r.object_id,r.library_id FROM descendants d JOIN edges r ON r.subject_id=d.id AND r.library_id=d.library_id
		WHERE r.relation_type='composed_of'
	)
	SELECT id FROM owners UNION SELECT e.id FROM owners o JOIN edges r ON r.subject_id=o.id AND r.library_id=o.library_id
	JOIN entities e ON e.id=r.object_id AND e.space_id=r.space_id AND e.library_id=r.library_id
	WHERE r.relation_type='has_certification' AND e.kind='certification' AND e.archived_at IS NULL AND e.merged_into_id=''`,
		spaceID, libraries, at, at, spaceID, roots, libraries).Scan(&owners).Error
	return owners, err
}

func (s *searchService) attachShelfSearchVersions(ctx context.Context, spaceID uint64, results []*types.EntrySearchResult) error {
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.EntryID)
	}
	var versions []types.ShelfDocumentVersion
	if err := s.db.WithContext(ctx).Select("id", "library_id", "file_id", "entry_id", "version_label", "ordinal", "is_current").
		Where("space_id=? AND entry_id IN ?", spaceID, ids).Find(&versions).Error; err != nil {
		return err
	}
	byEntry := map[string]types.ShelfDocumentVersion{}
	for _, version := range versions {
		byEntry[*version.EntryID] = version
	}
	for _, result := range results {
		if version, ok := byEntry[result.EntryID]; ok && version.LibraryID == result.LibraryID {
			result.ShelfDocument = &types.EntrySearchDocument{FileID: version.FileID, VersionID: version.ID,
				VersionLabel: version.VersionLabel, Ordinal: version.Ordinal, IsCurrent: version.IsCurrent}
		}
	}
	return nil
}

// Keep each version's evidence on its own entry. The current version represents
// the file when it matched; otherwise use the highest ranked historical hit.
func mergeShelfSearchVersions(results []*types.EntrySearchResult) []*types.EntrySearchResult {
	groups := map[string][]*types.EntrySearchResult{}
	for _, result := range results {
		if result.ShelfDocument != nil {
			groups[result.ShelfDocument.FileID] = append(groups[result.ShelfDocument.FileID], result)
		}
	}
	merged := make([]*types.EntrySearchResult, 0, len(results))
	for _, result := range results {
		if result.ShelfDocument == nil {
			merged = append(merged, result)
			continue
		}
		fileID := result.ShelfDocument.FileID
		group, ok := groups[fileID]
		if !ok {
			continue
		}
		delete(groups, fileID)
		representative := group[0]
		for _, member := range group {
			if member.ShelfDocument.IsCurrent {
				representative = member
				break
			}
		}
		score := group[0].Score
		for _, member := range group {
			representative.HistoricalMatch = representative.HistoricalMatch || !member.ShelfDocument.IsCurrent
			if member != representative {
				representative.OtherVersionMatches = append(representative.OtherVersionMatches, member)
			}
		}
		representative.Score = score
		merged = append(merged, representative)
	}
	slices.SortStableFunc(merged, compareEntryScores)
	return merged
}
