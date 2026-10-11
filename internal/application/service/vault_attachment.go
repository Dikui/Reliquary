package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// OpenAttachment resolves an article-relative image inside its own attachment directory.
func (v *VaultService) OpenAttachment(ctx context.Context, id, reference string) (*os.File, error) {
	space, ok := types.SpaceIDFromContext(ctx)
	if !ok || space == 0 {
		return nil, werrors.NewUnauthorizedError("authenticated attachment reader required")
	}
	var knowledge types.Knowledge
	if err := v.db.WithContext(ctx).Where("id=? AND space_id=? AND source_kind='article'", id, space).First(&knowledge).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("article not found")
		}
		return nil, err
	}
	if knowledge.VaultPath == "" || reference == "" || filepath.IsAbs(reference) || strings.Contains(reference, "\\") {
		return nil, werrors.NewBadRequestError("invalid article attachment path")
	}
	relative := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(knowledge.VaultPath)), filepath.FromSlash(reference))))
	if !strings.HasPrefix(relative, "attachments/"+knowledge.ID+"/") {
		return nil, werrors.NewBadRequestError("attachment path must belong to the article")
	}
	abs, err := vaultAbsolute(v.root, relative)
	if err != nil {
		return nil, werrors.NewBadRequestError(fmt.Sprintf("invalid article attachment: %v", err))
	}
	file, err := os.Open(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, werrors.NewNotFoundError("article attachment not found")
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, werrors.NewBadRequestError("attachment must be a regular file")
	}
	return file, nil
}
