package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	werrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

func (s *entryService) convertIntakeToMine(ctx context.Context, kb *types.KnowledgeBase, k *types.Entry, before types.EntryIntakeMetadata) (*types.Entry, error) {
	if s.vault == nil || s.db == nil || before.OriginalPath == "" {
		return nil, werrors.NewBadRequestError("stored original and configured vault are required")
	}
	read := func(ctx context.Context, path string) (io.ReadCloser, error) {
		return s.resolveFileServiceForPath(ctx, kb, path).GetFile(ctx, path)
	}
	file, err := read(ctx, before.OriginalPath)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, werrors.NewBadRequestError("stored original is empty")
	}
	title := strings.TrimSuffix(k.Title, filepath.Ext(k.FileName))
	name := vaultSlug(title) + ".md"
	// Stage a non-Markdown file so the mine watcher cannot index it before
	// the deletion and operation log commit.
	var relative, absolute string
	for i := 0; i < 100; i++ {
		if i > 0 {
			name = fmt.Sprintf("%s-%s-%d.md", vaultSlug(title), k.ID[:min(8, len(k.ID))], i)
		}
		relative = filepath.ToSlash(filepath.Join("mine", name))
		absolute, err = vaultAbsolute(s.vault.root, relative)
		if err != nil {
			return nil, err
		}
		_, err = os.Lstat(absolute)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
			break
		}
		if err != nil {
			return nil, err
		}
		err = os.ErrExist
	}
	if err != nil {
		return nil, err
	}
	destination, err := os.CreateTemp(filepath.Dir(absolute), ".intake-*.tmp")
	if err != nil {
		return nil, err
	}
	staged := destination.Name()
	committed := false
	defer func() {
		_ = destination.Close()
		if !committed {
			_ = os.Remove(staged)
			_ = os.RemoveAll(filepath.Join(s.vault.root, "attachments", k.ID))
		}
	}()
	if err := destination.Chmod(0o644); err != nil {
		return nil, err
	}
	body, err := s.vault.copyArticleImages(ctx, k, relative, string(data), read)
	if err != nil {
		return nil, err
	}
	if k.FilePath != "" {
		original, err := read(ctx, k.FilePath)
		if err != nil {
			return nil, err
		}
		bytes, err := io.ReadAll(original)
		_ = original.Close()
		if err != nil {
			return nil, err
		}
		path := filepath.ToSlash(filepath.Join("attachments", k.ID, vaultOriginalFileName(k.FileName)))
		if err := s.vault.writeAttachment(path, bytes); err != nil {
			return nil, err
		}
	}
	if _, err := destination.WriteString(body); err != nil {
		return nil, err
	}
	if err := destination.Sync(); err != nil {
		return nil, err
	}
	if err := destination.Close(); err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "review_convert_to_mine", k.ID, before, map[string]any{"vault_path": relative}); err != nil {
			return err
		}
		if err := deleteEntryAssociations(ctx, tx, k.SpaceID, []string{k.ID}); err != nil {
			return err
		}
		return repository.NewEntryRepository(tx).HardDeleteEntry(ctx, k.SpaceID, k.ID)
	})
	if err != nil {
		return nil, err
	}
	committed = true
	// A hard link publishes the staged bytes atomically and never replaces a
	// document created by the user while the transaction was running.
	if err := os.Link(staged, absolute); err != nil {
		return nil, fmt.Errorf("conversion committed; document remains staged at %s: %w", staged, err)
	}
	if err := os.Remove(staged); err != nil {
		logger.Errorf(ctx, "Converted intake %s staging cleanup failed: %v", k.ID, err)
	}
	documents, err := NewVaultDocumentService(s.db, s.modelService, s.retrieveEngine, s.vault.root, time.Minute, time.Minute)
	if err != nil {
		return nil, err
	}
	defer documents.Close()
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	mine, err := documents.indexDocument(ctx, kb, relative, "mine", body, info.ModTime())
	if err != nil {
		return nil, fmt.Errorf("personal document saved; indexing failed: %w", err)
	}
	if err := s.cleanupConvertedIntakeSource(ctx, k, before); err != nil {
		// The mine file and its index are already authoritative. Retaining a
		// temporary source is safer than rolling back a completed conversion.
		logger.Errorf(ctx, "Converted intake %s source cleanup failed: %v", k.ID, err)
	}
	return mine, nil
}

func (s *entryService) cleanupConvertedIntakeSource(ctx context.Context, k *types.Entry, intake types.EntryIntakeMetadata) error {
	releaser := entryResourceOwners(s.resourceCatalog, k.ID)
	seen := map[string]bool{}
	var cleanupErr error
	for _, ref := range append([]string{k.FilePath, intake.OriginalPath}, intake.Images...) {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		if releaser.deletable(ctx, ref) {
			cleanupErr = errors.Join(cleanupErr, s.resolveFileServiceForPath(ctx, nil, ref).DeleteFile(ctx, ref))
		}
	}
	return cleanupErr
}
