package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/config"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestIKB07LibrariesShareHeadersAndRebuild(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Millisecond)
	require.NoError(t, h.db.Model(&types.Library{}).Where("space_id=1").Update("indexing_strategy", types.IndexingStrategy{KeywordEnabled: true}).Error)
	custom := types.Library{ID: uuid.NewString(), SpaceID: 1, Name: "项目记录", Directory: "project-notes", Template: types.TemplateOwn, IndexingStrategy: types.IndexingStrategy{KeywordEnabled: true}}
	require.NoError(t, h.db.Create(&custom).Error)
	v, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: h.root, HeaderDebounce: time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, v.Close()) })
	var libraries []types.Library
	require.NoError(t, h.db.Order("directory").Find(&libraries).Error)
	require.Len(t, libraries, 6)
	bodies := map[string]string{}
	headers := map[string][]string{}
	var old *types.Entry
	image := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6xQAAAABJRU5ErkJggg=="
	for _, library := range libraries {
		body := "# " + library.Name + "\n\n完整正文首段。\n\n![图](" + image + ")\n\n正文末段，不能替换为摘要。"
		var entry *types.Entry
		if library.Template == types.TemplateGenerated {
			result, err := h.documents.createReport(h.ctx, &library, "测试 / "+library.Name, body, nil)
			require.NoError(t, err)
			entry = result.Entry
		} else {
			entry = &types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: library.ID, Title: "测试 / " + library.Name, SourceKind: "article", Type: types.EntryTypeManual, ParseStatus: types.ParseStatusCompleted, Acquisition: "verbatim", ContentType: "other", CreatedAt: time.Now().UTC()}
			if library.Template == types.TemplateOwn {
				entry.SourceKind = "mine"
				entry.SourceRef = "project/source"
			}
			if library.Template == types.TemplateExternalDurable {
				entry.Identifier = "doi:10.7/example"
			}
			if library.Directory == "official" {
				entry.SourceUnverified = true
			}
			require.NoError(t, h.db.Create(entry).Error)
			require.NoError(t, v.WriteArticle(h.ctx, entry, body, nil))
		}
		if library.Directory == "personal" {
			old = entry
		}
		require.Equal(t, library.Directory, filepath.Dir(entry.VaultPath))
		require.NoError(t, v.RefreshHeader(h.ctx, entry.ID))
		data, err := os.ReadFile(filepath.Join(h.root, entry.VaultPath))
		require.NoError(t, err)
		head, full, err := SplitVaultArticle(data)
		require.NoError(t, err)
		require.Contains(t, string(full), "正文末段")
		require.NotContains(t, string(full), "data:image")
		require.Contains(t, string(full), "../attachments/"+entry.ID+"/")
		images, err := os.ReadDir(filepath.Join(h.root, "attachments", entry.ID))
		require.NoError(t, err)
		require.Len(t, images, 1)
		var node yaml.Node
		require.NoError(t, yaml.Unmarshal(head[4:len(head)-4], &node))
		keys := []string{}
		for i := 0; i < len(node.Content[0].Content); i += 2 {
			keys = append(keys, node.Content[0].Content[i].Value)
		}
		headers[library.Directory] = keys
		expected := []string{"id", "library_id", "library", "title", "collected_at"}
		if entry.ContentType != "" {
			expected = append(expected, "content_type")
		}
		expected = append(expected, "starred", "acquisition")
		if entry.SourceRef != "" {
			expected = append(expected, "source_ref")
		}
		if entry.Identifier != "" {
			expected = append(expected, "identifier")
		}
		if entry.SourceUnverified {
			expected = append(expected, "source_unverified")
		}
		if entry.SourceRef != "" {
			expected = append(expected, "latest_entry_id")
		}
		if entry.SourceKind == "report" {
			expected = append(expected, "cannot_be_fact", "report_fingerprint")
		}
		require.Equal(t, expected, keys)
		require.NotContains(t, string(head), ": null")
		bodies[entry.ID] = string(full)
	}
	require.NotNil(t, old)
	newer := &types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: old.LibraryID, Title: old.Title, SourceKind: "mine", SourceRef: old.SourceRef, Type: types.EntryTypeManual, ParseStatus: types.ParseStatusCompleted, CreatedAt: time.Now().UTC().Add(time.Second)}
	require.NoError(t, h.db.Create(newer).Error)
	require.NoError(t, v.WriteArticle(h.ctx, newer, "新记录的完整正文", nil))
	require.NoError(t, v.RefreshHeader(h.ctx, old.ID))
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(h.root, old.VaultPath))
		return strings.Contains(string(data), "has_update: true") && strings.Contains(string(data), newer.ID)
	}, time.Second*3, time.Millisecond*10)
	require.NoError(t, v.Flush(h.ctx))
	before := vaultFileHashes(t, h.root)
	path := filepath.Join(h.root, old.VaultPath)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), "title:", "edited_title:", 1)), 0o600))
	require.NoError(t, v.RebuildFiles(h.ctx))
	require.Equal(t, before, vaultFileHashes(t, h.root))
	for _, library := range libraries {
		require.NoError(t, os.RemoveAll(filepath.Join(h.root, library.Directory)))
	}
	require.NoError(t, v.RebuildFiles(h.ctx))
	require.Equal(t, before, vaultFileHashes(t, h.root), "all files match after deleting every library directory")
	check, err := v.Verify(h.ctx, false)
	require.NoError(t, err)
	require.True(t, check.OK, check.Differences)
	fresh := pgtest.Open(t)
	rebuild, err := NewVaultDocumentService(fresh, nil, nil, h.root)
	require.NoError(t, err)
	_, err = rebuild.RebuildReplica(h.ctx)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(h.ctx, fresh, h.root))
	for id, body := range bodies {
		var entry types.Entry
		require.NoError(t, fresh.First(&entry, "id=?", id).Error)
		require.Equal(t, body, entry.VaultBody)
	}
	manifestPath := filepath.Join(h.root, ".reliquary/manifest.json")
	originalManifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var legacyManifest map[string]any
	require.NoError(t, json.Unmarshal(originalManifest, &legacyManifest))
	delete(legacyManifest, "registry_version")
	legacyBytes, err := json.Marshal(legacyManifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, legacyBytes, 0o600))
	_, err = rebuild.RebuildReplica(h.ctx)
	require.ErrorContains(t, err, "legacy file-library replica")
	require.NoError(t, os.WriteFile(manifestPath, originalManifest, 0o600))
	encoded, err := json.Marshal(headers)
	require.NoError(t, err)
	t.Log(string(encoded))
}
