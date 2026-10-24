package service

import (
	"context"
	"os"
	"path/filepath"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVaultVerifyRepairsAllLibraryFilesAndReplica(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	article := h.upload(t, "Verify recovery", "# Verified article\n\nDatabase can restore this original.")

	documents, err := NewVaultDocumentService(h.db, nil, h.service.retrieveEngine, v.root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, documents.Close()) })
	indexing := *h.library
	indexing.IndexingStrategy.VectorEnabled = false
	var generated types.Library
	require.NoError(t, h.db.Where("system_role=?", "ai-output").First(&generated).Error)
	generated.IndexingStrategy.VectorEnabled = false
	require.NoError(t, h.db.Model(&generated).Update("indexing_strategy", generated.IndexingStrategy).Error)
	report, err := documents.createReport(
		h.ctx,
		&generated,
		"Report original",
		"# Report original\n\nDatabase recovery copy.",
		nil,
	)
	require.NoError(t, err)
	personal := createMaintenanceMineFixture(t, h.ctx, documents, v, &indexing)
	require.NoError(t, documents.scanReports(h.ctx, &indexing, true))
	require.NoError(t, v.Flush(h.ctx))
	require.NoError(t, os.WriteFile(filepath.Join(v.root, article.VaultPath), []byte("damaged article"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(v.root, ".reliquary/data/entities.json"), []byte("invalid"), 0o600))
	require.NoError(t,
		os.WriteFile(filepath.Join(v.root,
			personal.VaultPath),
			[]byte("damaged personal document"),
			0o600))
	require.NoError(t, os.Remove(filepath.Join(v.root, report.VaultPath)))
	check, err := v.Verify(h.ctx, false)
	require.NoError(t, err)
	require.False(t, check.OK)
	check, err = v.Verify(h.ctx, true)
	require.NoError(t, err)
	require.True(t, check.OK, check.Differences)
	require.Empty(t, check.Differences)
	require.Contains(t, check.Repaired, report.VaultPath)
	require.Contains(t, check.Repaired, personal.VaultPath)
	require.Contains(t, check.Repaired, article.VaultPath)
	_, body, err := SplitVaultArticle(mustVaultRead(t, filepath.Join(v.root, article.VaultPath)))
	require.NoError(t, err)
	require.Equal(t, article.VaultBody, string(body))
	_, personalBody, err := SplitVaultArticle(mustVaultRead(t, filepath.Join(v.root, personal.VaultPath)))
	require.NoError(t, err)
	require.Equal(t, personal.VaultBody, string(personalBody))
	_, err = os.Stat(filepath.Join(v.root, report.VaultPath))
	require.NoError(t, err)
	// Broken JSONL is quarantined rather than overwritten in place.
	require.NoError(t,
		os.WriteFile(filepath.Join(v.root,
			".reliquary/operation_logs.jsonl"),
			[]byte("broken log"),
			0o600))
	_, err = v.Verify(h.ctx, true)
	require.NoError(t, err)
	quarantined, err := filepath.Glob(filepath.Join(v.root, ".reliquary/quarantine/*.corrupt.jsonl"))
	require.NoError(t, err)
	require.Len(t, quarantined, 1)
	require.Equal(t, "broken log", string(mustVaultRead(t, quarantined[0])))
}

func mustVaultRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func createMaintenanceMineFixture(t *testing.T, ctx context.Context, documents *VaultDocumentService, vault *VaultService, library *types.Library) *types.Entry {
	t.Helper()
	original := library
	var own types.Library
	require.NoError(t, documents.db.Where("system_role=?", "personal").First(&own).Error)
	own.IndexingStrategy = original.IndexingStrategy
	own.ChunkingConfig = original.ChunkingConfig
	own.ModelTiers = original.ModelTiers
	library = &own
	header := VaultArticleHeader{
		ID:          uuid.NewString(),
		SourceKind:  "mine",
		Title:       "Personal tutorial",
		CollectedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Summary: &types.EntrySummary{
			Overview: "Personal operating procedure",
			Points:   []string{"Preserve the full procedure"},
			KeyTerms: []string{"Recovery"},
		},
	}
	body := "# Personal tutorial\n\nmaintenanceminefixture full original procedure.\n"
	path := "personal/Personal-tutorial-" + header.ID[:8] + ".md"
	entry, err := documents.restoreVaultArticle(ctx, library, path, header, []byte(body))
	require.NoError(t, err)
	require.NoError(t, documents.indexVaultChunks(ctx, library, entry, body))
	header, err = vault.articleHeader(ctx, entry)
	require.NoError(t, err)
	require.NoError(t,
		os.WriteFile(filepath.Join(vault.root,
			path),
			append(RenderVaultArticleHeader(header),
				[]byte(body)...),
			0o600))
	attachment := filepath.Join(vault.root, "attachments", entry.ID, "fixture.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(attachment), 0o755))
	image, err := readVaultImage(ctx, articleImageDataURI(t, 10), nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(attachment, image, 0o600))
	return entry
}

func TestVaultReplicaRejectsPersonalBodyDisagreementBeforeImport(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	documents, err := NewVaultDocumentService(h.db, nil, h.service.retrieveEngine, v.root)
	require.NoError(t, err)
	indexing := *h.library
	indexing.IndexingStrategy.VectorEnabled = false
	personal := createMaintenanceMineFixture(t, h.ctx, documents, v, &indexing)
	snapshot, err := maintenance.Capture(h.ctx, h.db)
	require.NoError(t, err)
	for _, row := range snapshot.Tables["entries"] {
		if row["id"] == personal.ID {
			row["vault_body"] = "A different personal document body"
		}
	}
	require.NoError(t, maintenance.WriteSnapshot(v.root, "", snapshot))
	fresh := pgtest.Open(t)
	rebuilder, err := NewVaultDocumentService(fresh, nil, nil, v.root)
	require.NoError(t, err)
	_, err = rebuilder.RebuildReplica(context.Background())
	require.ErrorContains(t, err, "personal document body differs from replica")
	var count int64
	require.NoError(t, fresh.Model(&types.Entry{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestVaultReplicaRebuildListsUnmanagedPersonalFilesWithoutIndexing(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	documents, err := NewVaultDocumentService(h.db, nil, h.service.retrieveEngine, vault.root)
	require.NoError(t, err)
	indexing := *h.library
	indexing.IndexingStrategy.VectorEnabled = false
	personal := createMaintenanceMineFixture(t, h.ctx, documents, vault, &indexing)
	plainPath := "personal/hand-placed.md"
	require.NoError(t,
		os.WriteFile(filepath.Join(vault.root,
			plainPath),
			[]byte("# Hand placed note\n\nThis document was never imported."),
			0o600))
	unregisteredPath := "personal/unregistered.md"
	header := VaultArticleHeader{
		ID:          uuid.NewString(),
		SourceKind:  "mine",
		Title:       "Unregistered personal document",
		CollectedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	require.NoError(t,
		os.WriteFile(filepath.Join(vault.root,
			unregisteredPath),
			append(RenderVaultArticleHeader(header),
				[]byte("# Unregistered note\n\nThe readable replica contains no entry for this file.")...),
			0o600))
	require.NoError(t, vault.Flush(h.ctx))
	before := vaultFileHashes(t, vault.root)
	fresh := pgtest.Open(t)
	rebuilder, err := NewVaultDocumentService(fresh, nil, nil, vault.root)
	require.NoError(t, err)
	result, err := rebuilder.RebuildReplica(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Mine)
	require.Len(t, result.Skipped, 2)
	require.Equal(t, plainPath, result.Skipped[0].Path)
	require.Contains(t, result.Skipped[0].Error, "YAML front matter")
	require.Equal(t, unregisteredPath, result.Skipped[1].Path)
	require.Contains(t, result.Skipped[1].Error, "not recorded in the vault replica")
	var restored []types.Entry
	require.NoError(t, fresh.Where("source_kind='mine'").Find(&restored).Error)
	require.Len(t, restored, 1)
	require.Equal(t, personal.ID, restored[0].ID)
	require.Equal(t, personal.VaultBody, restored[0].VaultBody)
	require.Equal(t, before, vaultFileHashes(t, vault.root))
}

func TestVaultReplicaRebuildPreservesEveryPersistentTableAndUserFiles(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = articleProcessingOutput(t,
		[]legacyEntityCandidate{{
			Name:       "Company",
			EntityType: "organization",
		}})
	full := h.upload(t,
		"Research preservation",
		"# Original research\n\nImmutable original measurements and reasoning.")
	maintainTestEntities(h.ctx, t, h.db, full.ID, "Company")
	h.processor.output = articleProcessingOutput(t, nil)
	summary := h.upload(t, "Reference preservation", "# Summary reference\n\nComplete reference text is retained.")
	require.NoError(t, h.db.Model(full).Updates(map[string]any{"collect_reason": "User reason"}).Error)
	var entity types.Entity
	require.NoError(t, h.db.Where("canonical_name='Company'").First(&entity).Error)

	require.NoError(
		t,
		h.db.Exec("INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity',?)", entity.ID).Error,
	)
	require.NoError(
		t,
		h.db.Exec("INSERT INTO follow_feed_states(space_id,last_viewed_at) VALUES(1,'2026-10-10T09:00:00Z')").Error,
	)

	configuration := map[string]string{
		".obsidian/app.json": `{"theme":"obsidian"}`, ".trash/x.md": "# Deleted note\n", ".DS_Store": "Finder metadata",
		"personal/.trash/x.md": "# Deleted personal note\n", "ai-output/.obsidian/template.md": "# Editor template\n",
	}
	for relative, data := range configuration {
		path := filepath.Join(v.root, relative)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(data), 0o644))
	}
	documents, err := NewVaultDocumentService(h.db, nil, h.service.retrieveEngine, v.root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, documents.Close()) })
	// Build keyword chunks without calling the configured vector fixture.
	indexing := *h.library
	indexing.IndexingStrategy.VectorEnabled = false
	var generated types.Library
	require.NoError(t, h.db.Where("system_role=?", "ai-output").First(&generated).Error)
	generated.IndexingStrategy.VectorEnabled = false
	require.NoError(t, h.db.Model(&generated).Update("indexing_strategy", generated.IndexingStrategy).Error)
	report, err := documents.createReport(
		h.ctx,
		&generated,
		"Report original",
		"# Report original\n\nDatabase recovery copy.",
		nil,
	)
	require.NoError(t, err)
	_ = report
	importedPersonal := createMaintenanceMineFixture(t, h.ctx, documents, v, &indexing)
	require.NoError(t, documents.scanReports(h.ctx, &indexing, true))
	require.NoError(t, v.Flush(h.ctx))
	snapshot, err := maintenance.Capture(h.ctx, h.db)
	require.NoError(t, err)
	for _, row := range snapshot.Tables["entries"] {
		if row["source_kind"] == "mine" {
			require.Equal(t, importedPersonal.VaultBody, row["vault_body"])
		}
	}
	verified, err := v.Verify(h.ctx, false)
	require.NoError(t, err)
	require.True(t, verified.OK, "%v", verified.Differences)
	filesBefore := vaultFileHashes(t, v.root)
	before, err := maintenance.BuildManifest(context.Background(), h.db, v.root, "")
	require.NoError(t, err)
	fresh := pgtest.Open(t)
	rebuilder, err := NewVaultDocumentService(fresh, nil, nil, v.root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rebuilder.Close()) })
	result, err := rebuilder.RebuildReplica(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, result.Articles)
	require.EqualValues(t, 1, result.Mine)
	require.EqualValues(t, 1, result.Reports)
	after, err := maintenance.BuildManifest(context.Background(), fresh, v.root, "")
	require.NoError(t, err)
	require.Empty(t, maintenance.CompareManifest(before, after))
	for _, original := range []*types.Entry{full, summary, importedPersonal} {
		var rebuilt types.Entry
		require.NoError(t, fresh.Where("id=?", original.ID).First(&rebuilt).Error)
		require.Equal(t, original.VaultPath, rebuilt.VaultPath)
		require.Equal(t, original.VaultBody, rebuilt.VaultBody)
	}
	var personal []types.Entry
	require.NoError(t, fresh.Where("source_kind IN ('mine','report')").Find(&personal).Error)
	require.Len(t, personal, 2)
	for _, document := range personal {
		if document.SourceKind == "mine" {
			require.NotEmpty(t, document.VaultBody)
		} else {
			require.NotEmpty(t, document.VaultBody)
		}
	}
	require.Equal(t, filesBefore, vaultFileHashes(t, v.root), "replica rebuild preserves editor configuration and trash")
	for relative, data := range configuration {
		require.Equal(t, data, string(mustVaultRead(t, filepath.Join(v.root, relative))))
	}
}
