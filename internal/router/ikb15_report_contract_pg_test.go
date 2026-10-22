package router

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestIKB15FiveLibraryStarsAndOperationLogs(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	client := f.connect(t, "2025-11-25")
	for _, library := range []string{"official", "media", "references", "personal", "ai-output"} {
		t.Run(library, func(t *testing.T) {
			var id string
			acquisition := "verbatim"
			if library == "ai-output" {
				acquisition = "model_written"
			}
			var result types.IngestBatchResult
			mcpCall(t, client, "ingest", map[string]any{"items": []any{map[string]any{"library": library, "title": "Star " + library, "url": "https://example.test/" + library, "author": "Fixture author", "publisher": "Fixture publisher", "acquisition": acquisition, "content": "# " + library + "\n\nOriginal measured observations for " + library + "."}}}, &result)
			require.Equal(t, "accepted", result.Items[0].Status)
			id = result.Items[0].EntryID
			{
				f.queue.mu.Lock()
				task := f.queue.tasks[len(f.queue.tasks)-1]
				f.queue.mu.Unlock()
				require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, task))
			}
			var completed types.Entry
			require.NoError(t, f.db.First(&completed, "id=?", id).Error)
			require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
			var read service.EntryReadResult
			mcpCall(t, client, "get_entry", map[string]any{"id": id}, &read)
			require.Equal(t, library == "ai-output", read.CannotBeFact)
			for _, starred := range []bool{true, true, false} {
				var entry types.Entry
				mcpCall(t, client, "set_star", map[string]any{"id": id, "starred": starred}, &entry)
				require.Equal(t, starred, entry.Starred)
			}
			var logs []types.OperationLog
			require.NoError(t, f.db.Where("object_id=?", id).Order("created_at,id").Find(&logs).Error)
			require.Len(t, logs, 3)
			expected := "ingest"
			require.Equal(t, []string{expected, "star", "unstar"}, []string{logs[0].OperationType, logs[1].OperationType, logs[2].OperationType})
			for _, log := range logs {
				require.Contains(t, log.Actor, "api_key:")
			}
			{
				require.NoError(t, f.vault.RefreshHeader(f.ctx, id))
				var entry types.Entry
				require.NoError(t, f.db.First(&entry, "id=?", id).Error)
				file, err := os.ReadFile(filepath.Join(f.root, entry.VaultPath))
				require.NoError(t, err)
				header, _, err := service.ParseVaultArticle(file)
				require.NoError(t, err)
				require.False(t, header.Starred)
				require.Equal(t, library == "ai-output", header.CannotBeFact)
			}
		})
	}
}
