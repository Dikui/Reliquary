package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reliquary/internal/application/repository"
	"reliquary/internal/maintenance"
	"reliquary/internal/types"
	"reliquary/internal/vaultpath"
	"strings"
)

func (v *VaultService) StartReplica() error {
	if v.replica == nil {
		return nil
	}
	return v.replica.Start()
}

// Flush materializes the full file export for explicit export and legacy recovery.
func (v *VaultService) Flush(ctx context.Context) error {
	if !v.db.Migrator().HasColumn("libraries", "directory") {
		if v.replica == nil {
			return nil
		}
		return v.replica.Flush(ctx)
	}
	var ids []string
	if err := v.db.WithContext(ctx).Model(&types.Entry{}).Where("source_kind IN ('article','mine','report') AND vault_path<>'' AND parse_status<>'deleting'").Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := v.RefreshHeader(ctx, id); err != nil {
			return err
		}
	}
	if v.replica != nil {
		return v.replica.Flush(ctx)
	}
	return nil
}

func (v *VaultService) Verify(ctx context.Context, repair bool) (*maintenance.Verification, error) {
	return v.verify(ctx, repair, true)
}

func (v *VaultService) VerifyRuntime(ctx context.Context) (*maintenance.Verification, error) {
	return v.verify(ctx, false, false)
}

func (v *VaultService) verify(ctx context.Context, repair, exported bool) (*maintenance.Verification, error) {
	result := &maintenance.Verification{OK: true, Differences: []string{}, Repaired: []string{}}
	legacy := !v.db.Migrator().HasColumn("libraries", "directory")
	if legacy && repair {
		return result, fmt.Errorf("legacy vault repair requires its matching release")
	}
	// Historical offline verification must not assume migration 10 has run.
	embeddingModelSQL := "(SELECT s.embedding_model_id FROM spaces s WHERE s.id=b.space_id)"
	if !v.db.Migrator().HasColumn("spaces", "embedding_model_id") {
		embeddingModelSQL = "COALESCE(NULLIF(b.model_tiers->>'embedding',''),b.embedding_model_id)"
	}
	var pendingImages int64
	var articles []types.Entry
	if err := v.db.WithContext(ctx).Unscoped().Where("vault_path<>'' AND deleted_at IS NULL").Find(&articles).Error; err != nil {
		return result, err
	}
	for _, article := range articles {
		path, err := vaultAbsolute(v.root, article.VaultPath)
		if err != nil {
			return result, err
		}
		if article.SourceKind == "report" && vaultpath.UserMetadata(article.VaultPath) {
			continue
		}
		if !exported && article.ParseStatus == types.ParseStatusCompleted && article.VaultBody == "" {
			result.Differences = append(result.Differences, article.VaultPath+": saved document body is missing from database")
		}
		if exported {
			actual, readErr := os.ReadFile(path)
			if legacy {
				if readErr != nil {
					result.Differences = append(result.Differences, article.VaultPath+": "+readErr.Error())
				}
				// The original replica's checksums and SQL rows are verified below.
				continue
			}
			if article.SourceKind != "article" && article.SourceKind != "mine" && article.SourceKind != "report" {
				if readErr != nil {
					result.Differences = append(result.Differences, article.VaultPath+": "+readErr.Error())
				} else if article.FileHash != "" && vaultContentHash(string(actual)) != article.FileHash {
					result.Differences = append(result.Differences, article.VaultPath+": user file differs from indexed hash")
				}
				continue
			}
			header, err := v.articleHeader(ctx, &article)
			if err != nil {
				return result, err
			}
			body := []byte(article.VaultBody)

			expected := append(RenderVaultArticleHeader(header), body...)
			if readErr != nil || !bytes.Equal(actual, expected) {
				if repair {
					if err := v.atomicArticleWrite(path, RenderVaultArticleHeader(header), body, nil); err != nil {
						return result, err
					}
					result.Repaired = append(result.Repaired, article.VaultPath)
				} else {
					result.Differences = append(result.Differences, article.VaultPath+": article file differs from database")
				}
			}
		}

		for _, reference := range articleImagePaths(&article) {
			reader, err := v.ReadImage(&article, reference)
			if err != nil {
				result.Differences = append(result.Differences, article.VaultPath+": image missing: "+reference)
				continue
			}
			data, readErr := io.ReadAll(reader)
			reader.Close()
			if readErr != nil {
				return result, readErr
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			name := filepath.Base(reference)
			if strings.HasPrefix(name, "image-") && len(name) >= 22 && name[6:22] != hash[:16] {
				result.Differences = append(result.Differences, article.VaultPath+": image content hash differs: "+reference)
			}
			var expected string
			if err := v.db.WithContext(ctx).Table("image_embeddings").Select("content_hash").Where("entry_id=? AND attachment_path=? AND model_id=(SELECT "+embeddingModelSQL+" FROM libraries b WHERE b.id=?)", article.ID, reference, article.LibraryID).Scan(&expected).Error; err != nil {
				return result, err
			}
			if expected != "" && expected != hash {
				result.Differences = append(result.Differences, article.VaultPath+": indexed image hash differs: "+reference)
			}
			if expected == "" {
				var supports bool
				if err := v.db.WithContext(ctx).Raw(`SELECT COALESCE((m.parameters #>> '{embedding_parameters,supports_images}')::boolean,false) FROM libraries b JOIN models m ON m.id=`+embeddingModelSQL+` WHERE b.id=? AND b.indexing_strategy->>'vector_enabled'='true'`, article.LibraryID).Scan(&supports).Error; err != nil {
					return result, err
				}
				if supports {
					pendingImages++
				}
			}
		}
	}
	if uploads := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR")); uploads != "" {
		var resources []types.StoredResource
		if err := v.db.WithContext(ctx).Where("provider='local' AND state='active' AND deleted_at IS NULL").Find(&resources).Error; err != nil {
			return result, err
		}
		for _, resource := range resources {
			relative := strings.TrimPrefix(resource.PhysicalPath, "local://")
			if filepath.IsAbs(relative) {
				resolved, err := filepath.Rel(uploads, relative)
				if err != nil {
					return result, err
				}
				relative = resolved
			}
			path, err := maintenance.SafePath(uploads, relative)
			if err != nil {
				return result, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				result.Differences = append(result.Differences, "upload missing: "+relative)
				continue
			}
			if resource.Size > 0 && int64(len(data)) != resource.Size {
				result.Differences = append(result.Differences, "upload size differs: "+relative)
			}
			if resource.ContentHash != "" && fmt.Sprintf("%x", sha256.Sum256(data)) != resource.ContentHash {
				result.Differences = append(result.Differences, "upload content hash differs: "+relative)
			}
		}
	}
	if exported && v.replica != nil {
		replica, err := maintenance.VerifyReplica(ctx, v.db, v.root, os.Getenv("LOCAL_STORAGE_BASE_DIR"), repair)
		if err != nil {
			return result, err
		}
		result.Differences = append(result.Differences, replica.Differences...)
		result.Repaired = append(result.Repaired, replica.Repaired...)
	}
	result.OK = len(result.Differences) == 0
	result.PendingVectors = map[string]int64{}
	if pendingImages > 0 {
		result.PendingVectors["images"] = pendingImages
	}
	for key, query := range map[string]string{
		"articles": `SELECT count(*) FROM entries k JOIN libraries b ON b.id=k.library_id WHERE k.deleted_at IS NULL AND k.source_kind IN ('article','mine') AND k.parse_status='completed' AND k.structured_summary IS NOT NULL AND b.indexing_strategy->>'vector_enabled'='true' AND NOT EXISTS(SELECT 1 FROM article_embeddings e WHERE e.entry_id=k.id AND e.model_id=EMBEDDING_MODEL_SQL)`,
		"chunks":   `SELECT count(*) FROM chunks c JOIN libraries b ON b.id=c.library_id WHERE c.deleted_at IS NULL AND c.is_enabled AND b.indexing_strategy->>'vector_enabled'='true' AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.chunk_id=c.id AND e.model_id=EMBEDDING_MODEL_SQL)`,
		"entities": `SELECT count(*) FROM entities e WHERE e.merged_into_id='' AND EXISTS(
 SELECT 1 FROM libraries b LEFT JOIN models m ON m.id=EMBEDDING_MODEL_SQL
 WHERE b.space_id=e.space_id AND b.deleted_at IS NULL AND b.indexing_strategy->>'vector_enabled'='true' AND (
 e.name_embedding IS NULL OR e.embedding_needs_rebuild OR m.id IS NULL OR m.type<>'Embedding'
 OR e.embedding_model_id<>m.id OR e.embedding_dimension<>vector_dims(e.name_embedding)
 OR (COALESCE(NULLIF((m.parameters #>> '{embedding_parameters,dimension}')::int,0),NULLIF((m.parameters->>'dimension')::int,0),0)>0
 AND e.embedding_dimension<>COALESCE(NULLIF((m.parameters #>> '{embedding_parameters,dimension}')::int,0),NULLIF((m.parameters->>'dimension')::int,0),0))))`,
	} {
		var count int64
		if err := v.db.WithContext(ctx).Raw(strings.ReplaceAll(query, "EMBEDDING_MODEL_SQL", embeddingModelSQL)).Scan(&count).Error; err != nil {
			return result, err
		}
		if count > 0 {
			result.PendingVectors[key] = count
		}
	}
	return result, nil
}

func (s *VaultDocumentService) RebuildReplica(ctx context.Context) (*VaultRebuildResult, error) {
	result := &VaultRebuildResult{Skipped: []VaultRebuildSkippedFile{}}
	if err := maintenance.RequireCurrentVaultReplica(s.root); err != nil {
		return result, err
	}
	original, err := maintenance.Load(s.root)
	if err != nil {
		return result, err
	}
	legacyEntryNames := original.UsesLegacyEntryNames()
	_, err = maintenance.Import(ctx, s.db, s.root, os.Getenv("LOCAL_STORAGE_BASE_DIR"), func(row map[string]any) error {
		path, _ := row["vault_path"].(string)
		if path == "" {
			return nil
		}
		abs, err := vaultAbsolute(s.root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("restore original %s: %w", path, err)
		}
		if row["source_kind"] != "article" && row["source_kind"] != "mine" && row["source_kind"] != "report" {
			row["vault_body"] = ""
			return nil
		}
		header, body, err := parseVaultReplicaHeader(data, legacyEntryNames)
		if err != nil {
			return err
		}
		if err := validateVaultRebuildHeader(header, body); err != nil {
			return err
		}
		if header.LibraryID != "" && header.LibraryID != fmt.Sprint(row["library_id"]) {
			return fmt.Errorf("header library differs from replica: %s", path)
		}
		if fmt.Sprint(row["id"]) != header.ID {
			return fmt.Errorf("article ID differs from replica: %s", path)
		}
		// Both readable copies must agree before any row is inserted.
		checks := map[string]any{"title": header.Title, "content_type": header.ContentType, "source_unverified": header.SourceUnverified, "starred": header.Starred, "collect_reason": header.CollectReason}
		if _, present := row["acquisition"]; present {
			checks["acquisition"] = header.Acquisition
		} else if header.Acquisition != "" {
			return fmt.Errorf("article acquisition differs from replica: %s", path)
		}
		for key, value := range map[string]string{"source_ref": header.SourceRef, "identifier": header.Identifier} {
			if _, present := row[key]; present {
				checks[key] = value
			}
		}
		for key, value := range checks {
			if text, ok := value.(string); ok && text == "" && row[key] == nil {
				continue
			}
			if row[key] != value {
				return fmt.Errorf("article header %s differs from replica field %s", path, key)
			}
		}

		if row["source_kind"] == "mine" {
			if original, present := row["vault_body"]; present && original != string(body) {
				return fmt.Errorf("personal document body differs from replica: %s", path)
			}
		}
		row["vault_body"] = string(body)
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := maintenance.VerifyImportedReplica(ctx, s.db, s.root); err != nil {
		return result, err
	}
	if err := ensureVaultDirectories(s.db.WithContext(ctx), s.root); err != nil {
		return result, err
	}
	// Keyword reconstruction uses each entry's library while model credentials
	// are being entered. Vector rebuilding is a separate maintenance step.
	var articles []types.Entry
	if err := s.db.WithContext(ctx).Where("vault_path<>'' AND parse_status=?", types.ParseStatusCompleted).Find(&articles).Error; err != nil {
		return result, err
	}
	for i := range articles {
		article := &articles[i]
		actual, err := repository.NewLibraryRepository(s.db).GetLibraryByID(ctx, article.LibraryID)
		if err != nil {
			return result, err
		}
		indexing := *actual
		indexing.IndexingStrategy.VectorEnabled = false
		path, err := vaultAbsolute(s.root, article.VaultPath)
		if err != nil {
			return result, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return result, err
		}
		body := string(data)
		if article.SourceKind == "article" || article.SourceKind == "mine" || article.SourceKind == "report" {
			_, content, err := SplitVaultArticle(data)
			if err != nil {
				return result, err
			}
			body = string(content)
			if article.SourceKind == "report" {
				result.Reports++
			} else if article.SourceKind == "mine" {
				result.Mine++
			} else {
				result.Articles++
			}
		} else {
			result.Reports++
		}

		if err := s.indexVaultChunks(ctx, &indexing, article, body); err != nil {
			return result, err
		}
	}
	if err := s.listReplicaUnmanagedFiles(ctx, result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *VaultDocumentService) listReplicaUnmanagedFiles(ctx context.Context, result *VaultRebuildResult) error {
	var paths []string
	if err := s.db.WithContext(ctx).Unscoped().Model(&types.Entry{}).
		Where("vault_path<>''").Pluck("vault_path", &paths).Error; err != nil {
		return err
	}
	managed := make(map[string]bool, len(paths))
	for _, path := range paths {
		managed[path] = true
	}
	var libraries []types.Library
	if err := s.db.WithContext(ctx).Order("directory").Find(&libraries).Error; err != nil {
		return err
	}
	for _, library := range libraries {
		directory, err := vaultAbsolute(s.root, library.Directory)
		if err != nil {
			return err
		}
		err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(s.root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if vaultpath.UserMetadata(relative) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") || managed[relative] {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil {
				var header VaultArticleHeader
				var body []byte
				header, body, err = ParseVaultArticle(data)
				if err == nil {
					err = validateVaultRebuildHeader(header, body)
				}
				if err == nil {
					err = fmt.Errorf("document is not recorded in the vault replica")
				}
			}
			result.Skipped = append(result.Skipped, VaultRebuildSkippedFile{Path: relative, Error: err.Error()})
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func HasVaultReplica(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".reliquary", "manifest.json"))
	return err == nil
}
