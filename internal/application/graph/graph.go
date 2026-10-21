package graph

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

// Lock serializes graph mutations in a space. Call before locking entry rows.
// Entity maintenance acquires its existing identity barrier before this lock.
func Lock(tx *gorm.DB, spaceID uint64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "reliquary:graph:"+strconv.FormatUint(spaceID, 10)).Error
}

type changes struct {
	tx   *gorm.DB
	rows map[string]types.Relation
}

func newChanges(tx *gorm.DB) *changes { return &changes{tx: tx, rows: map[string]types.Relation{}} }
func (c *changes) touch(r types.Relation) {
	if old, ok := c.rows[r.ID]; ok && old.Version == 0 {
		return
	}
	if old, ok := c.rows[r.ID]; !ok || old.Version < r.Version {
		c.rows[r.ID] = r
	}
}
func (c *changes) flush() error {
	ids := []string{}
	for id := range c.rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		old := c.rows[id]
		var row types.Relation
		err := c.tx.Where("id=?", id).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err == nil {
			if e := c.tx.Model(&types.RelationSource{}).Where("relation_id=?", id).Count(&count).Error; e != nil {
				return e
			}
		}
		version := old.Version + 1
		if old.Version == 0 {
			version = 1
		}
		deleted := count == 0
		if deleted {
			if e := c.tx.Where("id=?", id).Delete(&types.Relation{}).Error; e != nil {
				return e
			}
		} else {
			if e := c.tx.Model(&types.Relation{}).Where("id=?", id).Updates(map[string]any{"version": version, "updated_at": time.Now()}).Error; e != nil {
				return e
			}
		}
		endpoints := map[string]bool{old.SubjectID: true, old.ObjectID: true}
		if !deleted {
			endpoints[row.SubjectID] = true
			endpoints[row.ObjectID] = true
		}
		entities := []string{}
		for entity := range endpoints {
			entities = append(entities, entity)
		}
		sort.Strings(entities)
		for _, entity := range entities {
			pending := types.GraphPendingSync{ID: uuid.NewString(), SpaceID: old.SpaceID, EntityID: entity, RelationID: id, RelationVersion: version, Deleted: deleted, CreatedAt: time.Now()}
			if e := c.tx.Create(&pending).Error; e != nil {
				return e
			}
		}
	}
	return nil
}
func (c *changes) removeEntries(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []types.Relation
	if err := c.tx.Model(&types.Relation{}).Joins("JOIN relation_sources s ON s.relation_id=relations.id").Where("s.entry_id IN ?", ids).Select("relations.*").Distinct().Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		c.touch(r)
	}
	return c.tx.Where("entry_id IN ?", ids).Delete(&types.RelationSource{}).Error
}

// MapType follows durable library-local aliases and rejects corrupt cycles.
func MapType(tx *gorm.DB, libraryID, name string) (string, error) {
	seen := map[string]bool{}
	for {
		if seen[name] {
			return "", errors.New("relation type merge cycle")
		}
		seen[name] = true
		var row types.RelationTypeMerge
		err := tx.Where("library_id=? AND from_type=?", libraryID, name).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
		name = row.ToType
	}
}
func canonicalEntity(tx *gorm.DB, spaceID uint64, id string) (string, error) {
	seen := map[string]bool{}
	for {
		if seen[id] {
			return "", errors.New("entity merge cycle")
		}
		seen[id] = true
		var row types.Entity
		if err := tx.Select("id,merged_into_id").Where("id=? AND space_id=?", id, spaceID).First(&row).Error; err != nil {
			return "", err
		}
		if row.MergedIntoID == "" {
			return row.ID, nil
		}
		id = row.MergedIntoID
	}
}
func Enabled(tx *gorm.DB, entry *types.Entry) (bool, error) {
	var library types.Library
	if err := tx.Select("id,template").Where("id=? AND space_id=?", entry.LibraryID, entry.SpaceID).First(&library).Error; err != nil {
		return false, err
	}
	if !library.ExtractsEntities() {
		return false, nil
	}
	if entry.Graph != nil {
		return *entry.Graph, nil
	}
	var row types.LibraryContentType
	err := tx.Where("library_id=? AND name=? AND enabled=true", entry.LibraryID, entry.ContentType).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return row.ExtractGraph, err
}
func mergeEvidence(a, b []types.RelationEvidence) []types.RelationEvidence {
	// Preserve all speakers and locations, including repeated evidence positions.
	return append(a, b...)
}
func (c *changes) add(entry *types.Entry, output *types.EntryProcessingOutput) error {
	enabled, err := Enabled(c.tx, entry)
	if err != nil {
		return err
	}
	if !enabled || output == nil {
		return nil
	}
	for _, input := range output.Relations {
		a, err := canonicalEntity(c.tx, entry.SpaceID, input.SubjectID)
		if err != nil {
			return err
		}
		b, err := canonicalEntity(c.tx, entry.SpaceID, input.ObjectID)
		if err != nil {
			return err
		}
		if a == b {
			continue
		}
		name, err := MapType(c.tx, entry.LibraryID, input.Type)
		if err != nil {
			return err
		}
		var suppressed int64
		if err := c.tx.Model(&types.RelationSuppression{}).Where("library_id=? AND subject_id=? AND relation_type=? AND object_id=?", entry.LibraryID, a, name, b).Count(&suppressed).Error; err != nil {
			return err
		}
		if suppressed > 0 {
			continue
		}
		// Deep copy evidence because saved processing results retain their original identity.
		evidence := make([]types.RelationEvidence, len(input.Evidence))
		for i, e := range input.Evidence {
			evidence[i] = e
			if e.Speaker != nil {
				speaker := *e.Speaker
				evidence[i].Speaker = &speaker
				if speaker.EntityID != "" {
					id, err := canonicalEntity(c.tx, entry.SpaceID, speaker.EntityID)
					if err != nil {
						return err
					}
					evidence[i].Speaker.EntityID = id
				}
			}
		}
		var row types.Relation
		fresh := false
		err = c.tx.Where("library_id=? AND subject_id=? AND relation_type=? AND object_id=?", entry.LibraryID, a, name, b).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fresh = true
			row = types.Relation{ID: uuid.NewString(), SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, SubjectID: a, RelationType: name, ObjectID: b, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			// New rows start at one; flush retains one for the first accepted state.
			if err = c.tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		initial := row
		if fresh {
			initial.Version = 0
		}
		c.touch(initial)
		var source types.RelationSource
		err = c.tx.Where("relation_id=? AND entry_id=?", row.ID, entry.ID).First(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			source = types.RelationSource{RelationID: row.ID, EntryID: entry.ID, Evidence: evidence, RunID: output.RunID, BodyHash: output.BodyHash}
			if err = c.tx.Create(&source).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			source.Evidence = mergeEvidence(source.Evidence, evidence)
			if err = c.tx.Save(&source).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ReplaceTx accepts a saved processing output in the caller's transaction.
// Call Lock first; stale older source_ref records cannot restore their sources.
func ReplaceTx(tx *gorm.DB, entry *types.Entry, output *types.EntryProcessingOutput) error {
	c := newChanges(tx)
	latest, err := types.FindLatestSourceEntry(tx, entry.SpaceID, entry.LibraryID, entry.SourceRef)
	if err != nil {
		return err
	}
	if latest != nil && latest.ID != entry.ID && latest.ParseStatus != types.ParseStatusCompleted {
		return nil
	}
	if err := c.removeEntries([]string{entry.ID}); err != nil {
		return err
	}
	if latest != nil && latest.ID != entry.ID {
		return c.flush()
	}
	if latest != nil {
		var ids []string
		if err := tx.Model(&types.Entry{}).Where("space_id=? AND library_id=? AND source_ref=? AND id<>?", entry.SpaceID, entry.LibraryID, entry.SourceRef, entry.ID).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if err := c.removeEntries(ids); err != nil {
			return err
		}
	}
	if err := c.add(entry, output); err != nil {
		return err
	}
	return c.flush()
}
func WithdrawTx(tx *gorm.DB, ids []string) error {
	c := newChanges(tx)
	if err := c.removeEntries(ids); err != nil {
		return err
	}
	return c.flush()
}

// DeleteEntriesTx covers soft and hard deletion, including batch fallback.
func DeleteEntriesTx(tx *gorm.DB, spaceID uint64, ids []string, hard bool) error {
	if err := Lock(tx, spaceID); err != nil {
		return err
	}
	var entries []types.Entry
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id IN ?", spaceID, ids).Order("id")
	if hard {
		lookup = lookup.Unscoped()
	}
	if err := lookup.Find(&entries).Error; err != nil {
		return err
	}
	ids = []string{}
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	c := newChanges(tx)
	if err := c.removeEntries(ids); err != nil {
		return err
	}
	query := tx.Where("space_id=? AND id IN ?", spaceID, ids)
	if hard {
		query = query.Unscoped()
	}
	if err := query.Delete(&types.Entry{}).Error; err != nil {
		return err
	}
	restored := map[string]bool{}
	for _, entry := range entries {
		if entry.SourceRef == "" {
			continue
		}
		latest, err := types.FindLatestSourceEntry(tx, spaceID, entry.LibraryID, entry.SourceRef)
		if err != nil {
			return err
		}
		if latest == nil || restored[latest.ID] {
			continue
		}
		// Removing an older record must not disturb the current projection.
		wasLatest := entry.CreatedAt.After(latest.CreatedAt) || (entry.CreatedAt.Equal(latest.CreatedAt) && entry.ID > latest.ID)
		if !wasLatest {
			continue
		}
		restored[latest.ID] = true
		var output types.EntryProcessingOutput
		err = tx.Where("entry_id=? AND space_id=?", latest.ID, spaceID).First(&output).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if latest.ParseStatus != types.ParseStatusCompleted {
			continue
		}
		if latest.VaultBody != "" && output.BodyHash != fmt.Sprintf("%x", sha256.Sum256([]byte(latest.VaultBody))) {
			continue
		}
		if err := c.removeEntries([]string{latest.ID}); err != nil {
			return err
		}
		if err := c.add(latest, &output); err != nil {
			return err
		}
	}
	return c.flush()
}

// rewrite combines colliding triples, then removes self edges and suppressed edges.
func (c *changes) rewrite(spaceID uint64, libraryID, fromType, toType, fromEntity, toEntity string) error {
	q := c.tx.Where("space_id=?", spaceID)
	if libraryID != "" {
		q = q.Where("library_id=?", libraryID)
	}
	if fromEntity != "" {
		q = q.Where("subject_id=? OR object_id=?", fromEntity, fromEntity)
	} else {
		q = q.Where("relation_type=?", fromType)
	}
	var rows []types.Relation
	if err := q.Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, old := range rows {
		row := old
		if fromEntity != "" {
			if row.SubjectID == fromEntity {
				row.SubjectID = toEntity
			}
			if row.ObjectID == fromEntity {
				row.ObjectID = toEntity
			}
		} else {
			row.RelationType = toType
		}
		c.touch(old)
		var suppressed int64
		if err := c.tx.Model(&types.RelationSuppression{}).Where("library_id=? AND subject_id=? AND relation_type=? AND object_id=?", row.LibraryID, row.SubjectID, row.RelationType, row.ObjectID).Count(&suppressed).Error; err != nil {
			return err
		}
		if row.SubjectID == row.ObjectID || suppressed > 0 {
			if err := c.tx.Delete(&old).Error; err != nil {
				return err
			}
			continue
		}
		var target types.Relation
		err := c.tx.Where("library_id=? AND subject_id=? AND relation_type=? AND object_id=? AND id<>?", row.LibraryID, row.SubjectID, row.RelationType, row.ObjectID, row.ID).First(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := c.tx.Model(&old).Updates(map[string]any{"subject_id": row.SubjectID, "object_id": row.ObjectID, "relation_type": row.RelationType}).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		// Surviving version exceeds both predecessor versions.
		target.Version = max(target.Version, old.Version)
		c.touch(target)
		var sources []types.RelationSource
		if err := c.tx.Where("relation_id=?", old.ID).Find(&sources).Error; err != nil {
			return err
		}
		for _, source := range sources {
			var existing types.RelationSource
			err := c.tx.Where("relation_id=? AND entry_id=?", target.ID, source.EntryID).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				source.RelationID = target.ID
				if err := c.tx.Create(&source).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				existing.Evidence = mergeEvidence(existing.Evidence, source.Evidence)
				if err := c.tx.Save(&existing).Error; err != nil {
					return err
				}
			}
		}
		if err := c.tx.Delete(&old).Error; err != nil {
			return err
		}
	}
	return nil
}
func rewriteSuppressions(tx *gorm.DB, spaceID uint64, libraryID, fromType, toType, fromEntity, toEntity string) error {
	q := tx.Where("space_id=?", spaceID)
	if libraryID != "" {
		q = q.Where("library_id=?", libraryID)
	}
	if fromEntity != "" {
		q = q.Where("subject_id=? OR object_id=?", fromEntity, fromEntity)
	} else {
		q = q.Where("relation_type=?", fromType)
	}
	var rows []types.RelationSuppression
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	for _, old := range rows {
		row := old
		if fromEntity != "" {
			if row.SubjectID == fromEntity {
				row.SubjectID = toEntity
			}
			if row.ObjectID == fromEntity {
				row.ObjectID = toEntity
			}
		} else {
			row.RelationType = toType
		}
		if err := tx.Delete(&old).Error; err != nil {
			return err
		}
		if row.SubjectID == row.ObjectID {
			continue
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
func (c *changes) enforceSuppressions(spaceID uint64) error {
	var rows []types.Relation
	if err := c.tx.Model(&types.Relation{}).Joins("JOIN relation_suppressions s ON s.library_id=relations.library_id AND s.subject_id=relations.subject_id AND s.relation_type=relations.relation_type AND s.object_id=relations.object_id").Where("relations.space_id=?", spaceID).Select("relations.*").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		c.touch(row)
		if err := c.tx.Delete(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
func MergeEntityTx(tx *gorm.DB, spaceID uint64, from, to string) error {
	if err := Lock(tx, spaceID); err != nil {
		return err
	}
	if err := rewriteSuppressions(tx, spaceID, "", "", "", from, to); err != nil {
		return err
	}
	c := newChanges(tx)
	if err := c.rewrite(spaceID, "", "", "", from, to); err != nil {
		return err
	}
	// Speakers can occur in otherwise unrelated triples.
	var sources []types.RelationSource
	if err := tx.Model(&types.RelationSource{}).Joins("JOIN relations r ON r.id=relation_sources.relation_id").Where("r.space_id=? AND relation_sources.evidence @> ?::jsonb", spaceID, fmt.Sprintf(`[{"speaker":{"entity_id":%q}}]`, from)).Select("relation_sources.*").Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		for i := range source.Evidence {
			if speaker := source.Evidence[i].Speaker; speaker != nil && speaker.EntityID == from {
				speaker.EntityID = to
			}
		}
		var relation types.Relation
		if err := tx.Where("id=?", source.RelationID).First(&relation).Error; err != nil {
			return err
		}
		c.touch(relation)
		if err := tx.Save(&source).Error; err != nil {
			return err
		}
	}
	if err := c.enforceSuppressions(spaceID); err != nil {
		return err
	}
	return c.flush()
}
func MergeTypeTx(tx *gorm.DB, spaceID uint64, libraryID, from, to string) error {
	if err := Lock(tx, spaceID); err != nil {
		return err
	}
	mapped, err := MapType(tx, libraryID, to)
	if err != nil {
		return err
	}
	if mapped == from {
		return errors.New("relation type merge cycle")
	}
	to = mapped
	if err := rewriteSuppressions(tx, spaceID, libraryID, from, to, "", ""); err != nil {
		return err
	}
	c := newChanges(tx)
	if err := c.rewrite(spaceID, libraryID, from, to, "", ""); err != nil {
		return err
	}
	row := types.RelationTypeMerge{LibraryID: libraryID, FromType: from, ToType: to, CreatedAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "library_id"}, {Name: "from_type"}}, DoUpdates: clause.AssignmentColumns([]string{"to_type"})}).Create(&row).Error; err != nil {
		return err
	}
	if err := tx.Model(&types.LibraryRelationType{}).Where("library_id=? AND name=?", libraryID, from).Update("enabled", false).Error; err != nil {
		return err
	}
	if err := c.enforceSuppressions(spaceID); err != nil {
		return err
	}
	return c.flush()
}
func DeleteRelationTx(tx *gorm.DB, spaceID uint64, id, actor string) (*types.Relation, error) {
	if err := Lock(tx, spaceID); err != nil {
		return nil, err
	}
	var row types.Relation
	if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&row).Error; err != nil {
		return nil, err
	}
	suppression := types.RelationSuppression{SpaceID: spaceID, LibraryID: row.LibraryID, SubjectID: row.SubjectID, RelationType: row.RelationType, ObjectID: row.ObjectID, Actor: actor, CreatedAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&suppression).Error; err != nil {
		return nil, err
	}
	c := newChanges(tx)
	c.touch(row)
	if err := tx.Delete(&row).Error; err != nil {
		return nil, err
	}
	return &row, c.flush()
}
