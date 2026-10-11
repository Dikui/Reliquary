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

// Rebuild restores an empty knowledge store without modifying any vault file.
// The initialized personal space and its model choices are infrastructure rather
// than recoverable article data. A keyword-only knowledge base needs no model.
func (s *VaultDocumentService) Rebuild(ctx context.Context) (*VaultRebuildResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, kb, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Unscoped().Model(&types.Knowledge{}).Where("tenant_id=?", kb.TenantID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, fmt.Errorf("vault rebuild requires an empty knowledge database")
	}
	result := &VaultRebuildResult{Skipped: []VaultRebuildSkippedFile{}}
	seenArticleIDs := make(map[string]bool)
	articleDirectory, err := vaultAbsolute(s.root, "articles")
	if err != nil {
		return result, err
	}
	err = filepath.WalkDir(articleDirectory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
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
		if err != nil {
			result.Skipped = append(result.Skipped, VaultRebuildSkippedFile{rel, err.Error()})
			return nil
		}
		if seenArticleIDs[header.ID] {
			result.Skipped = append(result.Skipped, VaultRebuildSkippedFile{rel, "duplicate article id"})
			return nil
		}
		knowledge, err := s.restoreVaultArticle(ctx, kb, rel, header, body)
		if err != nil {
			return fmt.Errorf("restore article %s: %w", rel, err)
		}
		seenArticleIDs[header.ID] = true
		if knowledge.ProcessingMode == types.ProcessingModeFull {
			if err := s.indexVaultChunks(ctx, kb, knowledge, knowledge.VaultBody); err != nil {
				_ = s.db.WithContext(ctx).Model(knowledge).Updates(map[string]interface{}{
					"parse_status": types.ParseStatusFailed, "error_message": err.Error(),
				}).Error
				return fmt.Errorf("index rebuilt article %s: %w", rel, err)
			}
			if err := s.db.WithContext(ctx).Save(knowledge).Error; err != nil {
				return err
			}
		}
		if err := indexArticleEmbedding(ctx, s.db, s.models, kb, knowledge); err != nil {
			return fmt.Errorf("index rebuilt article summary %s: %w", rel, err)
		}
		result.Articles++
		return nil
	})
	if err != nil {
		return result, err
	}
	// Personal documents and reports use the same hash-based indexer, bypassing
	// the stabilization delay because rebuild reads a stopped file library.
	for _, kind := range []string{"mine", "report"} {
		if err := s.scanDirectory(ctx, kb, kind, true, &result.Skipped); err != nil {
			return result, err
		}
	}
	for _, target := range []struct {
		kind  string
		count *int64
	}{{"mine", &result.Mine}, {"report", &result.Reports}} {
		if err := s.db.WithContext(ctx).Model(&types.Knowledge{}).
			Where("tenant_id=? AND source_kind=? AND parse_status=?", kb.TenantID, target.kind, types.ParseStatusCompleted).
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

func (s *VaultDocumentService) restoreVaultArticle(ctx context.Context, kb *types.KnowledgeBase, path string, header VaultArticleHeader, body []byte) (*types.Knowledge, error) {
	collected, _ := time.Parse(time.RFC3339Nano, header.CollectedAt)
	mode := types.ProcessingModeForContentType(header.ContentType)
	if header.SourceUnverified || header.ProcessingMode == types.ProcessingModeFull {
		mode = types.ProcessingModeFull
	}
	if mode == types.ProcessingModeSummaryOnly {
		body = nil
	}
	now := time.Now().UTC()
	knowledge := &types.Knowledge{ID: header.ID, TenantID: kb.TenantID, KnowledgeBaseID: kb.ID,
		Type: types.KnowledgeTypeManual, SourceKind: "article", VaultPath: path, VaultBody: string(body),
		Source: header.URL, Title: header.Title, ContentType: header.ContentType, ContentTypeCorrected: true, ProcessingMode: mode,
		StructuredSummary: header.Summary, Starred: header.Starred, CollectReason: header.CollectReason,
		SourceUnverified: header.SourceUnverified,
		ParseStatus:      types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted,
		EnableStatus: "enabled", Channel: types.ChannelAPI, FileName: filepath.Base(path), FileType: "md",
		CreatedAt: collected.UTC(), UpdatedAt: now, ProcessedAt: &now,
		FileHash: vaultContentHash(string(body)), FileSize: int64(len(body))}
	if header.URL != "" {
		knowledge.Type = "url"
	}
	if header.Summary != nil {
		knowledge.Description = header.Summary.RenderText()
	}
	if err := knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Source: "vault", Decision: "approved",
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
		for _, entry := range entries {
			if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && strings.HasPrefix(entry.Name(), "original-") {
				knowledge.VaultOriginalPath = filepath.ToSlash(filepath.Join("attachments", header.ID, entry.Name()))
				knowledge.Type = "file"
				knowledge.FileName = strings.TrimPrefix(entry.Name(), "original-")
				knowledge.FileType = strings.TrimPrefix(strings.ToLower(filepath.Ext(knowledge.FileName)), ".")
				if info, err := entry.Info(); err == nil {
					knowledge.FileSize = info.Size()
				}
				break
			}
		}
	}
	setMessageFingerprint(knowledge, string(body))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(knowledge).Error; err != nil {
			return err
		}
		for _, name := range sortedUniqueNames(header.Topics) {
			var topic types.Topic
			err := tx.Where("tenant_id=? AND name=?", kb.TenantID, name).First(&topic).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				topic = types.Topic{ID: uuid.NewString(), TenantID: kb.TenantID, Name: name, CreatedAt: now, UpdatedAt: now}
				err = tx.Create(&topic).Error
			}
			if err != nil {
				return err
			}
			// Headers cannot distinguish model and user selections. Preserve all
			// recovered selections against later automatic classification.
			if err := tx.Create(&types.KnowledgeTopic{TenantID: kb.TenantID, KnowledgeID: knowledge.ID,
				TopicID: topic.ID, Source: types.TopicSourceUser, CreatedAt: collected.UTC()}).Error; err != nil {
				return err
			}
		}
		for _, link := range sortedUniqueNames(header.Entities) {
			name := strings.TrimSpace(link[2 : len(link)-2])
			entity, err := restoreVaultEntity(tx, kb.TenantID, name, now)
			if err != nil {
				return err
			}
			var existing int64
			if err := tx.Model(&types.KnowledgeEntity{}).Where("tenant_id=? AND knowledge_id=? AND entity_id=?", kb.TenantID, knowledge.ID, entity.ID).Count(&existing).Error; err != nil {
				return err
			}
			if existing == 0 {
				if err := tx.Create(&types.KnowledgeEntity{TenantID: kb.TenantID, KnowledgeID: knowledge.ID,
					EntityID: entity.ID, CreatedAt: collected.UTC()}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	return knowledge, err
}

// The header carries names rather than entity types or IDs. Restore exact
// canonical/alias matches and create otherwise unknown names as the other type.
func restoreVaultEntity(tx *gorm.DB, tenantID uint64, name string, now time.Time) (*types.Entity, error) {
	normalized := normalizeEntityName(name)
	var entity types.Entity
	err := tx.Where("tenant_id=? AND normalized_name=? AND merged_into_id=''", tenantID, normalized).
		Order("canonical_name, id").First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = tx.Table("entities").Joins("JOIN entity_aliases AS aliases ON aliases.entity_id=entities.id AND aliases.tenant_id=entities.tenant_id").
			Where("entities.tenant_id=? AND aliases.normalized_alias=? AND entities.merged_into_id=''", tenantID, normalized).
			Order("entities.canonical_name, entities.id").Select("entities.*").First(&entity).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		entity = types.Entity{ID: uuid.NewString(), TenantID: tenantID, CanonicalName: name, NormalizedName: normalized,
			EntityType: "other", EmbeddingNeedsRebuild: true, CreatedAt: now, UpdatedAt: now}
		err = tx.Create(&entity).Error
	}
	return &entity, err
}
