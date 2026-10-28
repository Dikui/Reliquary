package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// publishEntryRelations runs after body localization and all processing stages.
func (s *entryService) publishEntryRelations(ctx context.Context, id, runID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceID := types.MustSpaceIDFromContext(ctx)
		if err := lockEntitySpaceShared(tx, spaceID); err != nil {
			return err
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		var entry types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, id).First(&entry).Error; err != nil {
			return err
		}
		output, err := ReadEntryProcessingOutput(ctx, tx, spaceID, id)
		if err != nil {
			return err
		}
		if output == nil || output.RunID != runID {
			return errors.New("processing output changed before graph publication")
		}
		if entry.VaultBody != "" && output.BodyHash != fmt.Sprintf("%x", sha256.Sum256([]byte(entry.VaultBody))) {
			return errors.New("processing body changed before graph publication")
		}
		return graph.ReplaceTx(tx, &entry, output)
	})
}

type RelationSourceView struct {
	types.RelationSource
	Title string `json:"title"`
}
type EntityRelationView struct {
	types.Relation
	Direction   string               `json:"direction"`
	Peer        types.Entity         `json:"peer"`
	LibraryName string               `json:"library_name"`
	SourceCount int                  `json:"source_count"`
	Sources     []RelationSourceView `json:"sources"`
}
type RelationGroup struct {
	RelationType string               `json:"relation_type"`
	Relations    []EntityRelationView `json:"relations"`
}

func (s *EntityService) ListRelations(ctx context.Context, id, libraryID string) ([]RelationGroup, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	groups := []RelationGroup{}
	// One repeatable read snapshot keeps counts, versions and source rows coherent.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY").Error; err != nil {
			return err
		}
		var entity types.Entity
		if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("entity not found")
		} else if err != nil {
			return err
		}
		for entity.MergedIntoID != "" {
			id = entity.MergedIntoID
			if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&entity).Error; err != nil {
				return err
			}
		}
		q := tx.Where("space_id=? AND library_id=? AND (subject_id=? OR object_id=?)", spaceID, entity.LibraryID, id, id)
		if libraryID != "" {
			var count int64
			if err := tx.Model(&types.Library{}).Where("space_id=? AND id=?", spaceID, libraryID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return werrors.NewNotFoundError("library not found")
			}
			q = q.Where("library_id=?", libraryID)
		}
		var rows []types.Relation
		if err := q.Order("relation_type,library_id,subject_id,object_id").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			item := EntityRelationView{Relation: row, Direction: "outgoing", Sources: []RelationSourceView{}}
			peer := row.ObjectID
			if row.ObjectID == id {
				peer = row.SubjectID
				item.Direction = "incoming"
			}
			if err := tx.Where("id=? AND space_id=? AND library_id=?", peer, spaceID, entity.LibraryID).First(&item.Peer).Error; err != nil {
				return err
			}
			var library types.Library
			if err := tx.Select("name").Where("id=?", row.LibraryID).Take(&library).Error; err != nil {
				return err
			}
			item.LibraryName = library.Name
			var sources []types.RelationSource
			if err := tx.Where("relation_id=?", row.ID).Order("entry_id").Find(&sources).Error; err != nil {
				return err
			}
			for _, source := range sources {
				var entry types.Entry
				if err := tx.Select("id,title").Where("id=? AND space_id=?", source.EntryID, spaceID).First(&entry).Error; err != nil {
					return err
				}
				item.Sources = append(item.Sources, RelationSourceView{RelationSource: source, Title: entry.Title})
			}
			item.SourceCount = len(item.Sources)
			if len(groups) == 0 || groups[len(groups)-1].RelationType != row.RelationType {
				groups = append(groups, RelationGroup{RelationType: row.RelationType, Relations: []EntityRelationView{}})
			}
			i := len(groups) - 1
			groups[i].Relations = append(groups[i].Relations, item)
		}
		return nil
	})
	return groups, err
}
func (s *EntityService) DeleteRelation(ctx context.Context, id string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	actor, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return werrors.NewUnauthorizedError("authenticated editor required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := graph.DeleteRelationTx(tx, spaceID, id, actor.StorageID())
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("relation not found")
		}
		if err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "delete_relation", id, row, nil)
	})
}
func (s *EntityService) MergeRelationTypes(ctx context.Context, libraryID, from, to string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return werrors.NewUnauthorizedError("authenticated editor required")
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to || len([]rune(from)) > 100 || len([]rune(to)) > 100 {
		return werrors.NewBadRequestError("two distinct relation types required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var library types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, libraryID).Take(&library).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("library not found")
		} else if err != nil {
			return err
		}
		names := []string{from, to}
		sort.Strings(names)
		for _, name := range names {
			var row types.LibraryRelationType
			if err := tx.Where("library_id=? AND name=?", libraryID, name).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewBadRequestError("relation type must belong to this library")
			} else if err != nil {
				return err
			}
			if !row.Enabled {
				mapped, err := graph.MapType(tx, libraryID, name)
				if err != nil {
					return err
				}
				if mapped == name {
					return werrors.NewBadRequestError("relation type is disabled")
				}
			}
		}
		mapped, err := graph.MapType(tx, libraryID, from)
		if err != nil {
			return err
		}
		target, err := graph.MapType(tx, libraryID, to)
		if err != nil {
			return err
		}
		if mapped == target {
			return nil
		}
		if mapped != from {
			return werrors.NewBadRequestError("source relation type already merged")
		}
		if err := graph.MergeTypeTx(tx, spaceID, libraryID, from, to); err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "merge_relation_types", libraryID, map[string]any{"from_type": from, "to_type": to}, map[string]any{"from_type": from, "to_type": target})
	})
}
