package service

import (
	"context"
	"errors"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func optionalDocumentID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// Resolve the default identity only once, then retain its revision in the draft.
// A later upload cannot silently replace the file selected by another reviewer.
func (s *EntityService) shelfDocumentBefore(ctx context.Context, draft *types.ShelfChangeset, item *types.ShelfChangesetItem) (types.JSON, error) {
	op := shelfResolvedOperation(draft, item.Operation)
	library, err := managedLibrary(s.db, draft.SpaceID, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	if err = s.validateShelfDocument(ctx, library, op.Document); err != nil {
		return nil, err
	}
	resolved := *item.Operation.Document
	if resolved.OwnerID == "" {
		resolved.OwnerID = op.Document.OwnerID
	}
	item.Operation.Document = &resolved
	var file *types.Entity
	if op.EntityID != "" {
		found, err := s.shelfEntity(ctx, draft.LibraryID, op.EntityID)
		if err != nil {
			return nil, err
		}
		file = found
	} else if op.Document.RequirementID != "" || op.Document.CertificateNumber != "" {
		var versions []types.ShelfDocumentVersion
		query := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND owner_id=? AND COALESCE(requirement_id,'')=? AND is_current", draft.SpaceID, draft.LibraryID, op.Document.OwnerID, op.Document.RequirementID)
		if op.Document.RequirementID == "" {
			query = query.Where("certificate_number=?", op.Document.CertificateNumber)
		}
		err := query.Find(&versions).Error
		if err != nil {
			return nil, err
		}
		if len(versions) > 1 {
			return nil, apperrors.NewBadRequestError("uncertain: document classification matches multiple files")
		}
		if len(versions) == 1 {
			found, err := s.shelfEntity(ctx, draft.LibraryID, versions[0].FileID)
			if err != nil {
				return nil, err
			}
			file = found
		}
	}
	if file == nil {
		if op.Revision > 0 {
			return nil, apperrors.NewConflictError("document changed; reload before saving")
		}
		item.Operation.Revision = -1
		return types.JSON("null"), nil
	}
	if file.Kind != "document_file" || file.ArchivedAt != nil {
		return nil, apperrors.NewBadRequestError("document file must be active")
	}
	if op.Revision != 0 && op.Revision != file.Revision {
		return nil, apperrors.NewConflictError("document changed; reload before saving")
	}
	item.EntityID = file.ID
	item.Operation.EntityID = file.ID
	item.Operation.Revision = file.Revision
	versions, err := s.ShelfDocumentVersions(ctx, file.ID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 || versions[0].OwnerID != op.Document.OwnerID || documentIDValue(versions[0].RequirementID) != op.Document.RequirementID {
		return nil, apperrors.NewBadRequestError("uncertain: selected document does not match its owner and requirement")
	}
	return shelfJSON(versions[0]), nil
}

func (s *EntityService) writeShelfDocument(ctx context.Context, draft *types.ShelfChangeset, item *types.ShelfChangesetItem, op types.ShelfOperation) (*types.ShelfDocumentVersion, error) {
	input := op.Document
	if input.OwnerID == "" || input.Stage != "draft" && input.Stage != "final" || input.AttachmentID == "" && input.CloudURL == "" {
		return nil, apperrors.NewBadRequestError("document owner, draft/final stage and an original or cloud link are required")
	}
	var file *types.Entity
	var err error
	if op.EntityID == "" {
		confirmations := []types.ManagedNameConfirmation{}
		if item.Decision != nil {
			confirmations = item.Decision.NameConfirmations
		}
		file, err = s.createManagedEntity(ctx, draft.LibraryID, types.ManagedEntityInput{Name: input.Name, Kind: "document_file", NameConfirmations: confirmations}, item.EntityID)
		if err != nil {
			return nil, err
		}
		if _, err = s.SaveManagedRelation(ctx, draft.LibraryID, "", types.ManagedRelationInput{SubjectID: input.OwnerID, ObjectID: file.ID, RelationType: "has_document"}); err != nil {
			return nil, err
		}
		if input.RequirementID != "" {
			if _, err = s.SaveManagedRelation(ctx, draft.LibraryID, "", types.ManagedRelationInput{SubjectID: file.ID, ObjectID: input.RequirementID, RelationType: "fulfills"}); err != nil {
				return nil, err
			}
		}
	} else {
		file, err = s.UpdateManagedEntity(ctx, op.EntityID, types.ManagedEntityPatch{Revision: op.Revision})
		if err != nil {
			return nil, err
		}
	}
	var ordinal int64
	if err = s.db.WithContext(ctx).Model(&types.ShelfDocumentVersion{}).Where("file_id=?", file.ID).Select("COALESCE(MAX(ordinal),0)").Scan(&ordinal).Error; err != nil {
		return nil, err
	}
	ordinal++
	label := input.VersionLabel
	if label == "" {
		label = strconv.FormatInt(ordinal, 10)
	}
	now := time.Now()
	occurred := now
	if draft.Source.FileDate != nil {
		occurred = *draft.Source.FileDate
	}
	if input.OccurredAt != nil {
		occurred = *input.OccurredAt
	}
	version := &types.ShelfDocumentVersion{
		ID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(draft.ID+":"+item.ID)).String(),
		SpaceID: draft.SpaceID, LibraryID: draft.LibraryID, FileID: file.ID,
		OwnerID: input.OwnerID, RequirementID: optionalDocumentID(input.RequirementID),
		CertificateNumber: input.CertificateNumber,
		AttachmentID:      optionalDocumentID(input.AttachmentID), CloudURL: input.CloudURL,
		EntryID: optionalDocumentID(input.EntryID),
		Ordinal: ordinal, VersionLabel: label, Stage: input.Stage, IsCurrent: true,
		Origin: input.Origin, OccurredAt: occurred, CreatedAt: now,
		Source: draft.Source, ChangesetID: draft.ID, ParseStatus: "not_parsed",
		Conditions: append([]types.ShelfCondition{}, input.Conditions...),
	}
	if err = s.db.WithContext(ctx).Model(&types.ShelfDocumentVersion{}).Where("file_id=? AND is_current", file.ID).Update("is_current", false).Error; err != nil {
		return nil, err
	}
	if input.EntryID != "" {
		version.ParseStatus = "completed"
	}
	if err = s.db.WithContext(ctx).Create(version).Error; err != nil {
		return nil, err
	}
	if input.EntryID != "" {
		if err = s.SetEntryEntityLink(ctx, input.EntryID, file.ID, false, nil); err != nil {
			return nil, err
		}
		if err = s.SetEntryEntityLink(ctx, input.EntryID, input.OwnerID, false, nil); err != nil {
			return nil, err
		}
	}
	return version, NewOperationLogService(s.db).RecordTx(ctx, s.db, "update_entity", file.ID, nil, map[string]any{"document_version": version})
}

func (s *EntityService) ShelfDocumentVersions(ctx context.Context, fileID string) ([]types.ShelfDocumentVersion, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var file types.Entity
	if err = s.db.WithContext(ctx).Where("id=? AND space_id=? AND kind='document_file'", fileID, spaceID).First(&file).Error; err != nil {
		return nil, managedWriteError(err)
	}
	rows := []types.ShelfDocumentVersion{}
	err = s.db.WithContext(ctx).Where("file_id=? AND space_id=?", fileID, spaceID).Order("ordinal DESC").Find(&rows).Error
	return rows, err
}

func (s *EntityService) OpenShelfDocumentVersion(ctx context.Context, fileID, versionID string) (*types.EntityAttachment, io.ReadCloser, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, nil, err
	}
	var version types.ShelfDocumentVersion
	err = s.db.WithContext(ctx).Where("id=? AND file_id=? AND space_id=?", versionID, fileID, spaceID).First(&version).Error
	if err != nil {
		return nil, nil, managedWriteError(err)
	}
	if version.AttachmentID == nil {
		return nil, nil, apperrors.NewNotFoundError("this version has no local original")
	}
	return s.OpenEntityAttachment(ctx, *version.AttachmentID)
}

func documentIDValue(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func (s *EntityService) validateShelfDocument(ctx context.Context, library *types.Library, input *types.ShelfDocumentInput) error {
	if input.Stage != "draft" && input.Stage != "final" {
		return apperrors.NewBadRequestError("document draft/final stage is required")
	}
	if input.AttachmentID == "" && input.CloudURL == "" {
		return apperrors.NewBadRequestError("document original or cloud link is required")
	}
	if input.CloudURL != "" {
		link, err := url.Parse(input.CloudURL)
		if err != nil || (link.Scheme != "https" && link.Scheme != "http") || link.Hostname() == "" || link.User != nil {
			return apperrors.NewBadRequestError("cloud_url must be an HTTP(S) link without credentials")
		}
	}
	for _, text := range []string{input.Name, input.VersionLabel, input.Origin, input.CertificateNumber, input.CloudURL} {
		if len([]rune(text)) > 20000 {
			return apperrors.NewBadRequestError("document text is too long")
		}
	}
	if input.OccurredAt != nil && input.OccurredAt.IsZero() {
		return apperrors.NewBadRequestError("document occurred_at must be a nonzero time")
	}
	if input.OwnerID == "" && input.CertificateNumber != "" {
		records, err := s.shelfCertificationEntities(ctx, library, time.Now())
		if err != nil {
			return err
		}
		matches := []string{}
		for _, entity := range records {
			var record types.ShelfCertification
			if raw, ok := entity.Attributes["certification"]; ok {
				if err = types.DecodeShelfValue(raw, &record); err != nil {
					return err
				}
				if record.Number == input.CertificateNumber {
					matches = append(matches, entity.ID)
				}
			}
		}
		if len(matches) != 1 {
			return apperrors.NewBadRequestError("uncertain: certificate number must identify exactly one certification record")
		}
		input.OwnerID = matches[0]
	}
	owner, err := s.shelfEntity(ctx, library.ID, input.OwnerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.NewBadRequestError("uncertain: document owner cannot be identified")
		}
		var app *apperrors.AppError
		if errors.As(err, &app) && app.HTTPCode == 404 {
			return apperrors.NewBadRequestError("uncertain: document owner cannot be identified")
		}
		return err
	}
	if owner.ArchivedAt != nil || !slices.Contains([]string{"model", "part", "platform", "product", "certification"}, owner.Kind) {
		return apperrors.NewBadRequestError("uncertain: invalid document owner kind or archived owner")
	}
	scope := owner
	if owner.Kind == "certification" {
		var record types.ShelfCertification
		if err = types.DecodeShelfValue(owner.Attributes["certification"], &record); err != nil {
			return apperrors.NewBadRequestError("uncertain: certification record cannot be identified")
		}
		if strings.TrimSpace(input.CertificateNumber) == "" || record.Number != input.CertificateNumber {
			return apperrors.NewBadRequestError("uncertain: certificate number must match the certification record")
		}
		scope, err = shelfCertificationOwner(s.db, library, record.OwnerID)
		if err != nil {
			return err
		}
	} else if input.CertificateNumber != "" {
		return apperrors.NewBadRequestError("uncertain: a numbered certificate must belong to its certification record")
	}
	if input.RequirementID != "" {
		var count int64
		if err = s.db.WithContext(ctx).Model(&types.Entity{}).Where("id=? AND space_id=? AND library_id=? AND kind='document_requirement' AND archived_at IS NULL AND merged_into_id=''", input.RequirementID, library.SpaceID, library.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return apperrors.NewBadRequestError("uncertain: document requirement cannot be identified")
		}
	}
	if input.AttachmentID != "" {
		var attachment types.EntityAttachment
		if err = s.db.WithContext(ctx).Where("id=? AND space_id=? AND library_id=?", input.AttachmentID, library.SpaceID, library.ID).First(&attachment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewBadRequestError("document original is missing or outside this library")
			}
			return err
		}
		if attachment.Size > library.ShelfConfig.FileSizeLimit() {
			return apperrors.NewBadRequestError("document original exceeds the configured file size limit")
		}
	}
	if input.EntryID != "" {
		var count int64
		if err = s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=? AND library_id=? AND vault_body<>''", input.EntryID, library.SpaceID, library.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return apperrors.NewBadRequestError("document text entry is missing or outside this library")
		}
		if err = s.db.WithContext(ctx).Model(&types.ShelfDocumentVersion{}).Where("entry_id=?", input.EntryID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewBadRequestError("each document version requires its own text entry")
		}
	}
	if err = types.ValidateShelfConditions(input.Conditions); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	if len(input.Conditions) > 0 {
		if scope.Kind == "part" {
			modules := []string{}
			at := time.Now()
			if err = s.db.WithContext(ctx).Model(&types.Relation{}).Where("library_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, scope.ID, at, at).Distinct("attributes->>'module_type'").Pluck("attributes->>'module_type'", &modules).Error; err != nil {
				return err
			}
			for _, condition := range input.Conditions {
				if slices.Contains(modules, condition.ModuleType) && !slices.Contains(condition.PartIDs, scope.ID) {
					return apperrors.NewBadRequestError("uncertain: part document conditions select a different part from its owner")
				}
			}
		}
		models, err := shelfCertificationModels(s.db, library, scope, time.Now())
		if err != nil {
			return err
		}
		for _, id := range models {
			err = validateShelfConditions(s.db, library, id, input.Conditions, time.Now())
			if err == nil {
				return nil
			}
			var app *apperrors.AppError
			if !errors.As(err, &app) {
				return err
			}
		}
		return apperrors.NewBadRequestError("uncertain: document conditions do not match one model in the owner scope")
	}
	return nil
}
