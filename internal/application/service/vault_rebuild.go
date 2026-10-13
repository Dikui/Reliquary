package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/vaultpath"
)

type VaultRebuildSkippedFile struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type VaultRebuildResult struct {
	Articles int                       `json:"articles"`
	Mine     int64                     `json:"mine"`
	Reports  int64                     `json:"reports"`
	Skipped  []VaultRebuildSkippedFile `json:"skipped"`
}

// Rebuild restores an empty entry store without modifying any vault file.
// The initialized personal space and its model choices are infrastructure rather
// than recoverable article data. A keyword-only library needs no model.
func (s *VaultDocumentService) Rebuild(ctx context.Context) (*VaultRebuildResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, library, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Unscoped().Model(&types.Entry{}).Where("space_id=?", library.SpaceID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, fmt.Errorf("vault rebuild requires an empty entry database")
	}
	result := &VaultRebuildResult{Skipped: []VaultRebuildSkippedFile{}}
	seenArticleIDs := make(map[string]bool)
	for _, directory := range []string{"articles", "mine"} {
		articleDirectory, err := vaultAbsolute(s.root, directory)
		if err != nil {
			return result, err
		}
		err = filepath.WalkDir(articleDirectory, func(path string, dirEntry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(s.root, path)
			if err != nil {
				return err
			}
			if vaultpath.UserMetadata(rel) {
				if dirEntry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if dirEntry.IsDir() || dirEntry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
				return nil
			}
			rel = filepath.ToSlash(rel)
			content, err := os.ReadFile(path)
			if err != nil {
				result.Skipped = append(result.Skipped, VaultRebuildSkippedFile{rel, err.Error()})
				return nil
			}
			header, body, err := ParseVaultArticle(content)
			if err == nil {
				err = validateVaultRebuildHeader(header, body)
			}
			if err == nil && directory == "mine" && header.SourceKind != "mine" {
				err = fmt.Errorf("personal document header requires source_kind: mine")
			}
			if err == nil && directory == "articles" && header.SourceKind != "" && header.SourceKind != "article" {
				err = fmt.Errorf("article directory contains a different source kind")
			}
			if err != nil {
				result.Skipped = append(result.Skipped, VaultRebuildSkippedFile{rel, err.Error()})
				return nil
			}
			if seenArticleIDs[header.ID] {
				result.Skipped = append(result.Skipped, VaultRebuildSkippedFile{rel, "duplicate article id"})
				return nil
			}
			entry, err := s.restoreVaultArticle(ctx, library, rel, header, body)
			if err != nil {
				return fmt.Errorf("restore article %s: %w", rel, err)
			}
			seenArticleIDs[header.ID] = true
			if entry.ProcessingMode == types.ProcessingModeFull {
				if err := s.indexVaultChunks(ctx, library, entry, entry.VaultBody); err != nil {
					_ = s.db.WithContext(ctx).Model(entry).Updates(map[string]interface{}{
						"parse_status": types.ParseStatusFailed, "error_message": err.Error(),
					}).Error
					return fmt.Errorf("index rebuilt article %s: %w", rel, err)
				}
				if err := s.db.WithContext(ctx).Save(entry).Error; err != nil {
					return err
				}
			}
			if err := indexArticleEmbedding(ctx, s.db, s.models, library, entry); err != nil {
				return fmt.Errorf("index rebuilt article summary %s: %w", rel, err)
			}
			vault := &VaultService{db: s.db, root: s.root}
			if err := indexArticleImages(ctx, s.db, s.models, library, vault, entry); err != nil {
				return fmt.Errorf("index rebuilt article images %s: %w", rel, err)
			}
			if directory == "articles" {
				result.Articles++
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	if err := s.scanReports(ctx, library, true, &result.Skipped); err != nil {
		return result, err
	}
	for _, target := range []struct {
		kind  string
		count *int64
	}{{"mine", &result.Mine}, {"report", &result.Reports}} {
		if err := s.db.WithContext(ctx).Model(&types.Entry{}).
			Where("space_id=? AND source_kind=? AND parse_status=?", library.SpaceID, target.kind, types.ParseStatusCompleted).
			Count(target.count).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}

func validateVaultRebuildHeader(header VaultArticleHeader, body []byte) error {
	if _, err := uuid.Parse(header.ID); err != nil {
		return fmt.Errorf("article id must be a UUID: %w", err)
	}
	if strings.TrimSpace(header.Title) == "" || len([]rune(header.Title)) > 255 {
		return fmt.Errorf("article title is required and must fit 255 characters")
	}
	if _, err := time.Parse(time.RFC3339Nano, header.CollectedAt); err != nil {
		return fmt.Errorf("invalid collected_at: %w", err)
	}
	if header.ContentType != "" && types.NormalizeContentType(header.ContentType) != header.ContentType {
		return fmt.Errorf("invalid content_type %q", header.ContentType)
	}
	if header.ProcessingMode != "" && header.ProcessingMode != types.ProcessingModeFull && header.ProcessingMode != types.ProcessingModeSummaryOnly {
		return fmt.Errorf("invalid processing_mode %q", header.ProcessingMode)
	}
	if header.SourceUnverified && header.URL != "" {
		return fmt.Errorf("unverified source must not contain an original URL")
	}
	if header.Acquisition != "" && header.Acquisition != "verbatim" && header.Acquisition != "extracted" && header.Acquisition != "model_written" {
		return fmt.Errorf("invalid acquisition %q", header.Acquisition)
	}
	if !utf8.Valid(body) {
		return fmt.Errorf("article body must be valid UTF-8")
	}
	for _, name := range header.Entities {
		if !strings.HasPrefix(name, "[[") || !strings.HasSuffix(name, "]]") || strings.TrimSpace(name[2:len(name)-2]) == "" {
			return fmt.Errorf("invalid entity link %q", name)
		}
		if len([]rune(strings.TrimSpace(name[2:len(name)-2]))) > 512 {
			return fmt.Errorf("entity name exceeds 512 characters")
		}
	}
	for _, name := range header.Topics {
		if len([]rune(strings.TrimSpace(name))) > 255 {
			return fmt.Errorf("topic name exceeds 255 characters")
		}
	}
	return nil
}

func (s *VaultDocumentService) restoreVaultArticle(ctx context.Context, library *types.Library, path string, header VaultArticleHeader, body []byte) (*types.Entry, error) {
	collected, _ := time.Parse(time.RFC3339Nano, header.CollectedAt)
	mode := types.ProcessingModeForContentType(header.ContentType)
	if header.SourceKind == "mine" || header.SourceUnverified || header.ProcessingMode == types.ProcessingModeFull {
		mode = types.ProcessingModeFull
	}
	if mode == types.ProcessingModeSummaryOnly {
		body = nil
	}
	kind := "article"
	if header.SourceKind == "mine" {
		kind = "mine"
	}
	now := time.Now().UTC()
	entry := &types.Entry{ID: header.ID, SpaceID: library.SpaceID, LibraryID: library.ID,
		Type: types.EntryTypeManual, SourceKind: kind, VaultPath: path, VaultBody: string(body),
		Source: header.URL, Title: header.Title, ContentType: header.ContentType, ContentTypeCorrected: true, ProcessingMode: mode,
		StructuredSummary: header.Summary, Starred: header.Starred, CollectReason: header.CollectReason,
		SourceUnverified: header.SourceUnverified,
		Acquisition:      header.Acquisition,
		ParseStatus:      types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted,
		EnableStatus: "enabled", Channel: types.ChannelAPI, FileName: filepath.Base(path), FileType: "md",
		CreatedAt: collected.UTC(), UpdatedAt: now, ProcessedAt: &now,
		FileHash: vaultContentHash(string(body)), FileSize: int64(len(body))}
	if header.URL != "" {
		entry.Type = "url"
	}
	if header.Summary != nil {
		entry.Description = header.Summary.RenderText()
	}
	if err := entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: "vault", Decision: "approved",
		SourceInfo: types.SourceInfo{OriginalURL: header.URL, Publisher: header.Publisher,
			Author: header.Author, PublishedAt: header.PublishedAt}}); err != nil {
		return nil, err
	}
	if mode == types.ProcessingModeFull {
		attachmentDirectory, err := vaultAbsolute(s.root, filepath.ToSlash(filepath.Join("attachments", header.ID)))
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(attachmentDirectory)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, dirEntry := range entries {
			if !dirEntry.IsDir() && dirEntry.Type()&os.ModeSymlink == 0 && strings.HasPrefix(dirEntry.Name(), "original-") {
				entry.VaultOriginalPath = filepath.ToSlash(filepath.Join("attachments", header.ID, dirEntry.Name()))
				entry.Type = "file"
				entry.FileName = strings.TrimPrefix(dirEntry.Name(), "original-")
				entry.FileType = strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.FileName)), ".")
				if info, err := dirEntry.Info(); err == nil {
					entry.FileSize = info.Size()
				}
				break
			}
		}
	}
	setMessageFingerprint(entry, string(body))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		for _, name := range sortedUniqueNames(header.Topics) {
			var topic types.Topic
			err := tx.Where("space_id=? AND name=?", library.SpaceID, name).First(&topic).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				topic = types.Topic{ID: uuid.NewString(), SpaceID: library.SpaceID, Name: name, CreatedAt: now, UpdatedAt: now}
				err = tx.Create(&topic).Error
			}
			if err != nil {
				return err
			}
			// Headers cannot distinguish model and user selections. Preserve all
			// recovered selections against later automatic classification.
			if err := tx.Create(&types.EntryTopic{SpaceID: library.SpaceID, EntryID: entry.ID,
				TopicID: topic.ID, Source: types.TopicSourceUser, CreatedAt: collected.UTC()}).Error; err != nil {
				return err
			}
		}
		for _, link := range sortedUniqueNames(header.Entities) {
			name := strings.TrimSpace(link[2 : len(link)-2])
			entity, err := restoreVaultEntity(tx, library.SpaceID, name, now)
			if err != nil {
				return err
			}
			var existing int64
			if err := tx.Model(&types.EntryEntity{}).Where("space_id=? AND entry_id=? AND entity_id=?", library.SpaceID, entry.ID, entity.ID).Count(&existing).Error; err != nil {
				return err
			}
			if existing == 0 {
				if err := tx.Create(&types.EntryEntity{SpaceID: library.SpaceID, EntryID: entry.ID,
					EntityID: entity.ID, CreatedAt: collected.UTC()}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	return entry, err
}

// The header carries names rather than entity types or IDs. Restore exact
// canonical/alias matches and create otherwise unknown names as the other type.
func restoreVaultEntity(tx *gorm.DB, spaceID uint64, name string, now time.Time) (*types.Entity, error) {
	normalized := normalizeEntityName(name)
	var entity types.Entity
	err := tx.Where("space_id=? AND normalized_name=? AND merged_into_id=''", spaceID, normalized).
		Order("canonical_name, id").First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = tx.Table("entities").Joins("JOIN entity_aliases AS aliases ON aliases.entity_id=entities.id AND aliases.space_id=entities.space_id").
			Where("entities.space_id=? AND aliases.normalized_alias=? AND entities.merged_into_id=''", spaceID, normalized).
			Order("entities.canonical_name, entities.id").Select("entities.*").First(&entity).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		entity = types.Entity{ID: uuid.NewString(), SpaceID: spaceID, CanonicalName: name, NormalizedName: normalized,
			EntityType: "other", EmbeddingNeedsRebuild: true, CreatedAt: now, UpdatedAt: now}
		err = tx.Create(&entity).Error
	}
	return &entity, err
}
