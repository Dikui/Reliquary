package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

func shelfTextID(versionID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("shelf-text:"+versionID)).String()
}

func (s *EntityService) shelfReparseVersion(ctx context.Context, draft *types.ShelfChangeset, op types.ShelfOperation) (*types.ShelfDocumentVersion, error) {
	if op.Reparse.VersionID == "" || op.Reparse.ParserKey == "" {
		return nil, apperrors.NewBadRequestError("version_id and parser_key are required")
	}
	file, err := s.shelfEntity(ctx, draft.LibraryID, op.EntityID)
	if err != nil {
		return nil, err
	}
	if file.Kind != "document_file" || file.ArchivedAt != nil {
		return nil, apperrors.NewBadRequestError("reparse requires an active document file")
	}
	if file.Revision != op.Revision {
		return nil, apperrors.NewConflictError("document changed; reload before reparsing")
	}
	var version types.ShelfDocumentVersion
	if err = s.db.WithContext(ctx).Where("id=? AND file_id=? AND library_id=? AND space_id=?", op.Reparse.VersionID, file.ID, draft.LibraryID, draft.SpaceID).First(&version).Error; err != nil {
		return nil, managedWriteError(err)
	}
	if version.AttachmentID == nil {
		return nil, apperrors.NewBadRequestError("reparse requires a local original; cloud links are not downloaded")
	}
	library, err := managedLibrary(s.db, draft.SpaceID, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	if _, ok := library.ShelfConfig.Parser(op.Reparse.ParserKey); !ok {
		return nil, apperrors.NewBadRequestError("selected parser is not configured in this library")
	}
	if version.EntryID != nil {
		if *version.EntryID != shelfTextID(version.ID) {
			return nil, apperrors.NewBadRequestError("uncertain: supplied independent text must be corrected by its submitter")
		}
		var jobs []queue.Job
		if err = s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("object_id=? AND state IN ?", *version.EntryID, []string{"active", "pending", "dispatched", "scheduled", "retry"}).Find(&jobs).Error; err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.State == queue.TaskStateActive {
				return nil, apperrors.NewConflictError("text processing is active; wait before reparsing")
			}
		}
	}
	return &version, nil
}

func (s *EntityService) writeShelfReparse(ctx context.Context, draft *types.ShelfChangeset, op types.ShelfOperation) (*types.ShelfDocumentVersion, error) {
	version, err := s.shelfReparseVersion(ctx, draft, op)
	if err != nil {
		return nil, err
	}
	before := *version
	if version.EntryID != nil {
		var entry types.Entry
		if err = s.db.WithContext(ctx).First(&entry, "id=? AND space_id=?", *version.EntryID, draft.SpaceID).Error; err != nil {
			return nil, err
		}
		// Reuse the ordinary body-replacement cleanup for text indexes and graph.
		if err = (&entryService{db: s.db}).cleanupLibraryRebuild(ctx, &entry); err != nil {
			return nil, err
		}
		if err = s.db.WithContext(ctx).Model(&queue.Job{}).Where("object_id=? AND state IN ?", entry.ID, []string{"pending", "dispatched", "scheduled", "retry"}).Updates(map[string]any{"state": "cancelled", "cancel_requested": true, "updated_at": time.Now()}).Error; err != nil {
			return nil, err
		}
		if err = s.db.WithContext(ctx).Model(&entry).Updates(map[string]any{"vault_body": "", "enable_status": "disabled", "parse_status": types.ParseStatusCancelled, "updated_at": time.Now()}).Error; err != nil {
			return nil, err
		}
		if err = s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
			return nil, err
		}
		version.EntryID = nil
	}
	if _, err = s.UpdateManagedEntity(ctx, op.EntityID, types.ManagedEntityPatch{Revision: op.Revision}); err != nil {
		return nil, err
	}
	version.ParserKey = op.Reparse.ParserKey
	version.ParseStatus = "not_parsed"
	version.ParseError = ""
	// Every request, including none, fences any older in-flight publication.
	version.ParseAttempt++
	if err = s.db.WithContext(ctx).Save(version).Error; err != nil {
		return nil, err
	}
	library, err := managedLibrary(s.db, draft.SpaceID, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	if err = s.enqueueShelfParse(ctx, version, library); err != nil {
		return nil, err
	}
	return version, NewOperationLogService(s.db).RecordTx(ctx, s.db, "reparse_document", op.EntityID, before, version)
}
