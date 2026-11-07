package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	fileService "reliquary/internal/application/service/file"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const MaxEntityAttachmentBytes = 32 << 20

// EntityAttachmentLimit keeps ordinary managed attachments at their existing
// limit while allowing each shelf to configure its original-file size.
func (s *EntityService) EntityAttachmentLimit(ctx context.Context, libraryID string) (int64, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return 0, err
	}
	library, err := managedLibrary(s.db.WithContext(ctx), spaceID, libraryID)
	if err != nil {
		return 0, err
	}
	if library.ShelfEnabled {
		return library.ShelfConfig.FileSizeLimit(), nil
	}
	return MaxEntityAttachmentBytes, nil
}

func (s *EntityService) SetEntityFileService(files interfaces.FileService) { s.files = files }

func (s *EntityService) UploadEntityAttachment(ctx context.Context, libraryID, name string, data []byte) (*types.EntityAttachment, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("attachment editor required")
	}
	if s.files == nil {
		return nil, apperrors.NewBadRequestError("file storage is unavailable")
	}
	limit, err := s.EntityAttachmentLimit(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "" || strings.ContainsAny(name, "\r\n\x00") || len([]rune(name)) > 255 || len(data) == 0 || int64(len(data)) > limit {
		return nil, apperrors.NewBadRequestError("attachment requires a valid name and nonempty content within the configured size limit")
	}
	row := &types.EntityAttachment{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: libraryID, Name: name, MimeType: http.DetectContentType(data), Size: int64(len(data)), CreatedAt: time.Now()}
	var stored []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		library, err := managedLibrary(tx, spaceID, libraryID)
		if err != nil {
			return err
		}
		currentLimit := int64(MaxEntityAttachmentBytes)
		if library.ShelfEnabled {
			currentLimit = library.ShelfConfig.FileSizeLimit()
		}
		if int64(len(data)) > currentLimit {
			return apperrors.NewBadRequestError("attachment exceeds the configured size limit")
		}
		catalog := NewResourceCatalog(repository.NewResourceRepository(tx))
		files := fileService.WithResourceCatalog(s.files, catalog, func(path string) { stored = append(stored, path) })
		path, err := files.SaveBytes(ctx, data, spaceID, name, false)
		if err != nil {
			return err
		}
		row.Path = path
		if _, ok := types.ParseResourcePath(path); ok {
			if err := catalog.Bind(ctx, path, "entity_attachment", row.ID, types.ResourceRelationAttachment); err != nil {
				return err
			}
		}
		return tx.Create(row).Error
	})
	if err != nil {
		// Remove only bytes created by this failed transaction, never historical attachments.
		for _, path := range stored {
			_ = s.files.DeleteFile(ctx, path)
		}
		return nil, managedWriteError(err)
	}
	return row, nil
}

func (s *EntityService) EntityAttachment(ctx context.Context, id string) (*types.EntityAttachment, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var row types.EntityAttachment
	err = s.db.WithContext(ctx).Where("id=? AND space_id=?", id, spaceID).First(&row).Error
	return &row, managedWriteError(err)
}

func (s *EntityService) DeleteEntityAttachment(ctx context.Context, id string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return apperrors.NewUnauthorizedError("attachment editor required")
	}
	if s.files == nil {
		return apperrors.NewBadRequestError("file storage is unavailable")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var row types.EntityAttachment
		if err := tx.Where("id=? AND space_id=?", id, spaceID).First(&row).Error; err != nil {
			return err
		}
		if _, err := managedLibrary(tx, spaceID, row.LibraryID); err != nil {
			return err
		}
		referenced, err := managedValueReferenced(tx, spaceID, id, "")
		if err != nil {
			return err
		}
		var versions int64
		if err := tx.Model(&types.ShelfDocumentVersion{}).Where("space_id=? AND attachment_id=?", spaceID, id).Count(&versions).Error; err != nil {
			return err
		}
		if referenced || versions > 0 {
			return apperrors.NewConflictError("attachment is still referenced by a product snapshot, relationship or document version")
		}
		catalog := NewResourceCatalog(repository.NewResourceRepository(tx))
		remaining, err := catalog.Release(ctx, row.Path, "entity_attachment", row.ID)
		if err != nil {
			return err
		}
		if remaining <= 0 {
			files := fileService.WithResourceCatalog(s.files, catalog, nil)
			if err := files.DeleteFile(ctx, row.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return tx.Delete(&row).Error
	})
	return managedWriteError(err)
}

func (s *EntityService) OpenEntityAttachment(ctx context.Context, id string) (*types.EntityAttachment, io.ReadCloser, error) {
	row, err := s.EntityAttachment(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if s.files == nil {
		return nil, nil, apperrors.NewBadRequestError("file storage is unavailable")
	}
	reader, err := s.files.GetFile(ctx, row.Path)
	return row, reader, err
}
