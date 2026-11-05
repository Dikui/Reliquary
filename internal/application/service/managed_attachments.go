package service

import (
	"context"
	"io"
	"net/http"
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
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "" || strings.ContainsAny(name, "\r\n\x00") || len([]rune(name)) > 255 || len(data) == 0 || len(data) > MaxEntityAttachmentBytes {
		return nil, apperrors.NewBadRequestError("attachment requires a valid name and 1 byte to 32 MiB of content")
	}
	row := &types.EntityAttachment{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: libraryID, Name: name, MimeType: http.DetectContentType(data), Size: int64(len(data)), CreatedAt: time.Now()}
	var stored []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := managedLibrary(tx, spaceID, libraryID); err != nil {
			return err
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
