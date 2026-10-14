package service

import (
	"context"
	"gorm.io/gorm"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/searchutil"
	"reliquary/internal/sourceloc"
	"reliquary/internal/types"
	"sort"
	"strings"
)

// Keep chunk boundaries while moving them with replaced image destinations.
// The normal processing task may later produce a fresh set of chunks.
func recalculateBodyLocations(ctx context.Context, db *gorm.DB, entry *types.Entry, oldBody, newBody string, replacements map[string]string) error {
	var chunks []*types.Chunk
	if err := db.WithContext(ctx).Where("space_id=? AND entry_id=?", entry.SpaceID, entry.ID).Find(&chunks).Error; err != nil {
		return err
	}
	old := []rune(oldBody)
	type edit struct{ start, end, size int }
	edits := []edit{}
	oldRefs, newRefs := bodyImageReferences(oldBody), bodyImageReferences(newBody)
	if len(oldRefs) != len(newRefs) {
		return nil
	}
	for i, ref := range oldRefs {
		next := newRefs[i].URL
		if next == ref.URL {
			continue
		}
		start := ref.Position + strings.Index(oldBody[ref.Position:], ref.URL)
		begin := len([]rune(oldBody[:start]))
		edits = append(edits, edit{begin, begin + len([]rune(ref.URL)), len([]rune(next))})
	}
	mapOffset := func(pos int) int {
		delta := 0
		for _, e := range edits {
			if pos <= e.start {
				break
			}
			if pos < e.end {
				return e.start + delta + (pos-e.start)*e.size/(e.end-e.start)
			}
			delta += e.size - (e.end - e.start)
		}
		return pos + delta
	}
	updated := []rune(newBody)
	for _, c := range chunks {
		if c.StartAt >= 0 && c.EndAt <= len(old) && c.EndAt > c.StartAt && string(old[c.StartAt:c.EndAt]) == c.Content {
			c.StartAt, c.EndAt = mapOffset(c.StartAt), mapOffset(c.EndAt)
			if c.StartAt < 0 || c.EndAt > len(updated) || c.StartAt > c.EndAt {
				c.SourceLocations = nil
				continue
			}
			c.Content = string(updated[c.StartAt:c.EndAt])
		} else {
			c.SourceLocations = nil
		}
	}
	sourceloc.Assign(newBody, chunks)
	for _, c := range chunks {
		if err := db.WithContext(ctx).Model(c).Select("content", "start_at", "end_at", "source_locations").Updates(c).Error; err != nil {
			return err
		}
	}
	return nil
}

// backfillBodyLocations adds positions to already saved bodies on upgrade.
func backfillBodyLocations(ctx context.Context, db *gorm.DB) error {
	var entries []types.Entry
	return db.WithContext(ctx).Where("vault_body<>'' AND EXISTS (SELECT 1 FROM chunks c WHERE c.entry_id=entries.id AND c.space_id=entries.space_id AND c.deleted_at IS NULL AND c.source_locations='[]'::jsonb)").FindInBatches(&entries, 100, func(tx *gorm.DB, _ int) error {
		for _, entry := range entries {
			var chunks []*types.Chunk
			if err := db.WithContext(ctx).Where("space_id=? AND entry_id=?", entry.SpaceID, entry.ID).Find(&chunks).Error; err != nil {
				return err
			}
			for _, c := range chunks {
				types.AssignStoredBodyLocations(entry.VaultBody, c)
				if len(c.SourceLocations) > 0 {
					if err := db.WithContext(ctx).Model(c).Select("source_locations").Updates(c).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	}).Error
}

// Match the image syntax already accepted by supplementation and indexing.
func bodyImageReferences(body string) []docparser.MarkdownImageReference {
	refs := docparser.MarkdownImageReferences(body)
	for _, match := range searchutil.HTMLImageSrcRegex.FindAllStringSubmatchIndex(body, -1) {
		group := searchutil.HTMLImageSrcURLGroup * 2
		if len(match) > group+1 && match[group] >= 0 {
			refs = append(refs, docparser.MarkdownImageReference{Position: match[group], URL: body[match[group]:match[group+1]]})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Position < refs[j].Position })
	return refs
}

// BackfillBodyLocations also serves offline SQL backup upgrades, whose chunks
// acquire the empty location column before the application next starts.
func BackfillBodyLocations(ctx context.Context, db *gorm.DB) error {
	if !db.Migrator().HasColumn("chunks", "source_locations") {
		return nil
	}
	return backfillBodyLocations(ctx, db)
}
