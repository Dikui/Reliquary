package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// ShelfDocuments filters versions before pagination, including their own text.
func (s *EntityService) ShelfDocuments(ctx context.Context, libraryID string, input types.ShelfDocumentListRequest) (*types.ShelfDocumentList, error) {
	var result *types.ShelfDocumentList
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		var err error
		result, err = reader.shelfDocuments(ctx, libraryID, input)
		return err
	})
	return result, err
}
func (s *EntityService) shelfDocuments(ctx context.Context, libraryID string, input types.ShelfDocumentListRequest) (*types.ShelfDocumentList, error) {
	library, err := s.shelfLibraryForRead(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return nil, apperrors.NewBadRequestError("invalid document pagination")
	}
	query := s.db.WithContext(ctx).Table("shelf_document_versions v").Joins("JOIN entities e ON e.id=v.file_id").Joins("LEFT JOIN entities owner ON owner.id=v.owner_id").Where("v.library_id=? AND v.space_id=? AND e.archived_at IS NULL AND e.merged_into_id=''", library.ID, library.SpaceID)
	if !input.IncludeHistory {
		query = query.Where("v.is_current")
	}
	switch input.Group {
	case "":
	case "requirements":
		query = query.Where("jsonb_array_length(v.requirement_ids)>0 AND owner.kind<>'certification'")
	case "certificates":
		query = query.Where("owner.kind='certification'")
	case "other":
		query = query.Where("jsonb_array_length(v.requirement_ids)=0 AND (owner.kind IS NULL OR owner.kind<>'certification')")
	default:
		return nil, apperrors.NewBadRequestError("invalid document group")
	}
	if input.OwnerKind == "unassigned" {
		query = query.Where("v.owner_id IS NULL")
	} else if input.OwnerKind != "" {
		query = query.Where("owner.kind=?", input.OwnerKind)
	}
	if input.ParseStatus != "" {
		query = query.Where("v.parse_status=?", input.ParseStatus)
	}
	if input.Query != "" {
		pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(input.Query) + "%"
		query = query.Where("(e.canonical_name ILIKE ? OR EXISTS (SELECT 1 FROM entries text WHERE text.id=v.entry_id AND text.deleted_at IS NULL AND text.vault_body ILIKE ?))", pattern, pattern)
	}
	result := &types.ShelfDocumentList{Items: []types.ShelfDocumentListItem{}}
	if err = query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	versions := []types.ShelfDocumentVersion{}
	if err = query.Select("v.*").Order("e.updated_at DESC,e.id,v.ordinal DESC").Limit(input.Limit).Offset(input.Offset).Scan(&versions).Error; err != nil {
		return nil, err
	}
	ids := []string{}
	for _, v := range versions {
		ids = append(ids, v.FileID, documentIDValue(v.OwnerID))
		ids = append(ids, v.Requirements()...)
	}
	entities := []types.Entity{}
	if len(ids) > 0 {
		if err = s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND id IN ?", library.SpaceID, library.ID, ids).Find(&entities).Error; err != nil {
			return nil, err
		}
	}
	pointers := []*types.Entity{}
	for i := range entities {
		pointers = append(pointers, &entities[i])
	}
	if err = hydrateManagedEntityAttributes(ctx, s.db, pointers, time.Now()); err != nil {
		return nil, err
	}
	byID := map[string]*types.Entity{}
	for i := range entities {
		byID[entities[i].ID] = &entities[i]
	}
	for _, v := range versions {
		requirements := []types.Entity{}
		for _, id := range v.Requirements() {
			if entity := byID[id]; entity != nil {
				requirements = append(requirements, *entity)
			}
		}
		result.Items = append(result.Items, types.ShelfDocumentListItem{Requirements: requirements, Entity: *byID[v.FileID], Document: v, Owner: byID[documentIDValue(v.OwnerID)], Requirement: byID[documentIDValue(v.RequirementID)]})
	}
	result.Truncated = int64(input.Offset+len(versions)) < result.Total
	return result, nil
}

func (s *EntityService) shelfDocumentEditBefore(ctx context.Context, draft *types.ShelfChangeset, op types.ShelfOperation) (types.JSON, error) {
	file, err := s.shelfEntity(ctx, draft.LibraryID, op.EntityID)
	if err != nil {
		return nil, err
	}
	if file.Kind != "document_file" || file.ArchivedAt != nil {
		return nil, apperrors.NewBadRequestError("an active document file is required")
	}
	if file.Revision != op.Revision {
		return nil, apperrors.NewConflictError("document changed; reload before saving")
	}
	versions, err := s.ShelfDocumentVersions(ctx, file.ID)
	if err != nil {
		return nil, err
	}
	if op.Type == "select_document_version" {
		for _, v := range versions {
			if v.ID == op.VersionID {
				return shelfJSON(versions), nil
			}
		}
		return nil, apperrors.NewBadRequestError("version must belong to this document")
	}
	if op.Classification.OwnerID == "" {
		return nil, apperrors.NewBadRequestError("uncertain: choose an explicit classification owner")
	}
	library, err := managedLibrary(s.db, draft.SpaceID, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	input := types.ShelfDocumentInput{OwnerID: op.Classification.OwnerID, RequirementID: op.Classification.RequirementID, RequirementIDs: slices.Clone(op.Classification.RequirementIDs), CertificateNumber: op.Classification.CertificateNumber, Conditions: op.Classification.Conditions}
	if err = s.validateShelfDocumentClassification(ctx, library, &input); err != nil {
		return nil, err
	}
	return shelfJSON(versions), nil
}

func (s *EntityService) writeShelfDocumentEdit(ctx context.Context, draft *types.ShelfChangeset, op types.ShelfOperation) (any, error) {
	before, err := s.shelfDocumentEditBefore(ctx, draft, op)
	if err != nil {
		return nil, err
	}
	if _, err = s.UpdateManagedEntity(ctx, op.EntityID, types.ManagedEntityPatch{Revision: op.Revision}); err != nil {
		return nil, err
	}
	if op.Type == "select_document_version" {
		if err = s.db.WithContext(ctx).Model(&types.ShelfDocumentVersion{}).Where("file_id=? AND is_current", op.EntityID).Update("is_current", false).Error; err != nil {
			return nil, err
		}
		if err = s.db.WithContext(ctx).Model(&types.ShelfDocumentVersion{}).Where("id=? AND file_id=?", op.VersionID, op.EntityID).Update("is_current", true).Error; err != nil {
			return nil, managedWriteError(err)
		}
		selection := types.ShelfDocumentSelection{ID: uuid.NewString(), SpaceID: draft.SpaceID, LibraryID: draft.LibraryID, FileID: op.EntityID, VersionID: op.VersionID, SelectedAt: time.Now()}
		if err = s.db.WithContext(ctx).Create(&selection).Error; err != nil {
			return nil, err
		}
	} else {
		versions, err := s.ShelfDocumentVersions(ctx, op.EntityID)
		if err != nil {
			return nil, err
		}
		c := op.Classification
		input := types.ShelfDocumentInput{RequirementID: c.RequirementID, RequirementIDs: c.RequirementIDs}
		if err := normalizeDocumentRequirements(&input); err != nil {
			return nil, err
		}
		// Classification belongs to the file. Its old values remain in this operation log.
		if err = s.db.WithContext(ctx).Model(&types.ShelfDocumentVersion{}).Where("file_id=?", op.EntityID).Updates(map[string]any{"owner_id": c.OwnerID, "requirement_id": optionalDocumentID(input.RequirementID), "requirement_ids": shelfJSON(input.RequirementIDs), "certificate_number": c.CertificateNumber, "conditions": shelfJSON(c.Conditions)}).Error; err != nil {
			return nil, managedWriteError(err)
		}
		var edges []types.Relation
		if err = s.db.WithContext(ctx).Where("library_id=? AND valid_to IS NULL AND ((object_id=? AND relation_type='has_document') OR (subject_id=? AND relation_type='fulfills'))", draft.LibraryID, op.EntityID, op.EntityID).Find(&edges).Error; err != nil {
			return nil, err
		}
		now := time.Now()
		for _, edge := range edges {
			if err = s.DeleteManagedRelation(ctx, edge.ID, edge.Version, now); err != nil {
				return nil, err
			}
		}
		if _, err = s.SaveManagedRelation(ctx, draft.LibraryID, "", types.ManagedRelationInput{SubjectID: c.OwnerID, ObjectID: op.EntityID, RelationType: "has_document"}); err != nil {
			return nil, err
		}
		for _, requirementID := range input.RequirementIDs {
			if _, err = s.SaveManagedRelation(ctx, draft.LibraryID, "", types.ManagedRelationInput{SubjectID: op.EntityID, ObjectID: requirementID, RelationType: "fulfills"}); err != nil {
				return nil, err
			}
		}
		for _, v := range versions {
			if v.EntryID == nil {
				continue
			}
			if previous := documentIDValue(v.OwnerID); previous != "" && previous != c.OwnerID {
				if err = s.SetEntryEntityLink(ctx, *v.EntryID, previous, true, nil); err != nil {
					return nil, err
				}
			}
			if err = s.SetEntryEntityLink(ctx, *v.EntryID, c.OwnerID, false, nil); err != nil {
				return nil, err
			}
		}
	}
	after, err := s.ShelfDocumentVersions(ctx, op.EntityID)
	if err != nil {
		return nil, err
	}
	return after, NewOperationLogService(s.db).RecordTx(ctx, s.db, op.Type, op.EntityID, before, after)
}
