package service

import (
	"context"
	"sort"
	"strings"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

// ListLibraryDuplicates groups entries in one library whose normalized bodies are
// identical, whatever their sources. Partial overlap is never a duplicate here.
func (s *entryService) ListLibraryDuplicates(ctx context.Context, libraryID string) ([]types.DuplicateGroup, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok || spaceID == 0 {
		return nil, apperrors.NewUnauthorizedError("personal space is required")
	}
	base := func() *gorm.DB {
		return s.db.WithContext(ctx).Model(&types.Entry{}).
			Where("space_id = ? AND library_id = ? AND content_hash <> ''", spaceID, libraryID).
			Where("parse_status NOT IN ?", []string{types.ParseStatusFailed, types.ManualEntryStatusDraft, types.ParseStatusDeleting, types.ParseStatusCancelled})
	}
	hashes := base().Select("content_hash").Group("content_hash").Having("COUNT(*) > 1")
	var entries []types.Entry
	if err := base().Select("id, title, source, metadata, content_hash, created_at").
		Where("content_hash IN (?)", hashes).Order("content_hash, created_at, id").Find(&entries).Error; err != nil {
		return nil, err
	}
	groups := []types.DuplicateGroup{}
	for start := 0; start < len(entries); {
		end := start
		for end < len(entries) && entries[end].ContentHash == entries[start].ContentHash {
			end++
		}
		members := make([]duplicateCandidate, 0, end-start)
		for i := start; i < end; i++ {
			members = append(members, newDuplicateCandidate(&entries[i]))
		}
		// Keep the earliest publication; a missing date falls back to intake time.
		sort.SliceStable(members, func(i, j int) bool { return members[i].at.Before(members[j].at) })
		group := types.DuplicateGroup{Keep: members[0].DuplicateEntry}
		for _, member := range members[1:] {
			group.Remove = append(group.Remove, member.DuplicateEntry)
		}
		groups = append(groups, group)
		start = end
	}
	return groups, nil
}

type duplicateCandidate struct {
	types.DuplicateEntry
	at time.Time
}

func newDuplicateCandidate(entry *types.Entry) duplicateCandidate {
	source, _ := entrySourceInfo(entry)
	item := duplicateCandidate{DuplicateEntry: types.DuplicateEntry{
		ID: entry.ID, Title: entry.Title, OriginalURL: source.OriginalURL, PublishedAt: source.PublishedAt, CreatedAt: entry.CreatedAt,
	}, at: entry.CreatedAt}
	if strings.TrimSpace(item.OriginalURL) == "" {
		item.OriginalURL = entry.Source
	}
	if published := parsePublicationTime(source.PublishedAt); published != nil {
		item.at = *published
	}
	return item
}
