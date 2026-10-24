package service

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestIKB08MergedVaultPositionsContentUpdateAndRestore(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Millisecond)
	require.NoError(t, h.db.Model(&types.Library{}).Where("space_id=1").Update("indexing_strategy", types.IndexingStrategy{KeywordEnabled: true}).Error)
	var library types.Library
	require.NoError(t, h.db.Where("directory='official'").First(&library).Error)
	v, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: h.root, HeaderDebounce: time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, v.Close()) })
	entry := &types.Entry{
		ID:           uuid.NewString(),
		SpaceID:      1,
		LibraryID:    library.ID,
		Type:         types.EntryTypeManual,
		SourceKind:   "article",
		Title:        "合并后定位 😀",
		ParseStatus:  types.ParseStatusCompleted,
		Acquisition:  "verbatim",
		EnableStatus: "enabled",
	}
	source := "https://example.org/long-original-image.png"
	body := "# 中文 😀\n\n首段 English。\n\n## 表格\n\n|列|值|\n|---|---|\n|一|two|\n\n- 列表一\n- item two\n\n```text\n# not a heading\n\ncode\n```\n\n![图](" + source + ")\n\n最后一段。"
	require.NoError(t, h.db.Create(entry).Error)
	require.NoError(t, v.WriteArticle(h.ctx, entry, body, nil))
	require.Equal(t, "official", filepath.Dir(entry.VaultPath))
	require.NoError(t, h.documents.indexVaultChunks(h.ctx, &library, entry, entry.VaultBody))
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6xQAAAABJRU5ErkJggg==")
	require.NoError(t, err)
	ref, err := v.StoreArticleAttachment(entry, "chart.png", image)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ref, "../attachments/"+entry.ID+"/"))
	next := strings.ReplaceAll(entry.VaultBody, source, ref)
	previous := entry.VaultBody
	change, err := v.beginArticleContentUpdate(entry, next)
	require.NoError(t, err)
	require.NoError(t, recalculateBodyLocations(h.ctx, h.db, entry, previous, next, nil))
	require.NoError(t, h.db.Model(entry).Update("vault_body", next).Error)
	require.NoError(t, change.finish())
	check := func(dbBody string, chunks []*types.Chunk) []byte {
		t.Helper()
		require.NoError(t, v.RefreshHeader(h.ctx, entry.ID))
		data, err := os.ReadFile(filepath.Join(h.root, entry.VaultPath))
		require.NoError(t, err)
		_, content, err := SplitVaultArticle(data)
		require.NoError(t, err)
		require.Equal(t, dbBody, string(content))
		require.NotEmpty(t, chunks)
		for _, c := range chunks {
			require.NotEmpty(t, c.SourceLocations)
			var extracted strings.Builder
			for _, loc := range c.SourceLocations {
				extracted.WriteString(string([]rune(dbBody)[loc.Start:loc.End]))
			}
			require.Equal(t, c.Content, extracted.String())
		}
		raw, err := json.Marshal(chunks)
		require.NoError(t, err)
		return raw
	}
	var chunks []*types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", entry.ID).Order("chunk_index").Find(&chunks).Error)
	before := check(next, chunks)
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", entry.ID).UpdateColumn("source_locations", "[]").Error)
	require.NoError(t, BackfillBodyLocations(h.ctx, h.db))
	var filled []*types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", entry.ID).Order("chunk_index").Find(&filled).Error)
	check(next, filled)
	for i := range chunks {
		require.Equal(t, chunks[i].SourceLocations, filled[i].SourceLocations)
	}
	// Backfill may update regenerated chunk timestamps. File rebuilding itself
	// must leave the entire current chunk snapshot unchanged.
	before = check(next, filled)
	require.NoError(t, v.Flush(h.ctx))
	require.NoError(t, os.RemoveAll(filepath.Join(h.root, library.Directory)))
	require.NoError(t, v.RebuildFiles(h.ctx))
	var after []*types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", entry.ID).Order("chunk_index").Find(&after).Error)
	require.Equal(t, before, check(next, after))
	fresh := pgtest.Open(t)
	restored, err := NewVaultDocumentService(fresh, nil, nil, h.root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restored.Close()) })
	_, err = restored.RebuildReplica(h.ctx)
	require.NoError(t, err)
	var rebuilt []*types.Chunk
	require.NoError(t, fresh.Where("entry_id=?", entry.ID).Order("chunk_index").Find(&rebuilt).Error)
	check(next, rebuilt)
}
