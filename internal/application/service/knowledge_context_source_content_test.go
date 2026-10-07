package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestContextSnapshotUsesSourceContentAfterChunkEdits(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		name := "legacy_backfill"
		if initialized {
			name = "parser_source_content"
		}
		t.Run(name, func(t *testing.T) {
			db := pgtest.Open(t)
			require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Evidence test team"}).Error)
			domain := BuiltinAIKnowledgeDomain()
			domain.ID, domain.TenantID = "source-domain", 1
			require.NoError(t, db.Create(domain).Error)
			require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: domain.ID,
				Name: "Evidence sources"}).Error)
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, types.UserIDContextKey, "editor")
			k := &types.Knowledge{ID: "source-message", TenantID: 1, KnowledgeBaseID: "kb", Type: "file",
				Title: "Source", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
				CreatedAt: time.Now(), UpdatedAt: time.Now()}
			require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
			require.NoError(t, db.Create(k).Error)
			intake, err := k.IntakeMetadata()
			require.NoError(t, err)
			require.Empty(t, intake.OriginalPath)

			original := "Parser output with **bold text** and [a link](https://example.com)."
			ending := "Second parser paragraph."
			first := &types.Chunk{ID: "first", TenantID: 1, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID,
				ChunkIndex: 0, ChunkType: types.ChunkTypeText, Content: original, IsEnabled: true, IndexStatus: "ready"}
			if initialized {
				first.SourceContent = original
			}
			second := &types.Chunk{ID: "second", TenantID: 1, KnowledgeID: k.ID, KnowledgeBaseID: k.KnowledgeBaseID,
				ChunkIndex: 1, ChunkType: types.ChunkTypeText, Content: ending, IsEnabled: true, IndexStatus: "ready"}
			repo := repository.NewChunkRepository(db)
			require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{second, first}))
			// New rows initialize SourceContent; clear it to model a legacy row.
			if !initialized {
				require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", first.ID).Update("source_content", "").Error)
			}
			beforeEdit, err := repo.GetChunkByID(ctx, k.TenantID, first.ID)
			require.NoError(t, err)
			if initialized {
				require.Equal(t, original, beforeEdit.SourceContent)
			} else {
				require.Empty(t, beforeEdit.SourceContent)
			}
			tasks := &intakeTask{}
			chunks := &chunkService{chunkRepository: repo, knowledgeRepo: repository.NewKnowledgeRepository(db),
				kbRepository: repository.NewKnowledgeBaseRepository(db), task: tasks}
			source := &knowledgeService{db: db, chunkService: chunks}
			var firstEvidence types.KnowledgeEvidence
			for revision, edit := range []string{"User replacement body.", "Another user replacement."} {
				updated, err := chunks.UpdateDocumentChunk(ctx, first.ID, &edit, nil, &revision)
				require.NoError(t, err)
				require.Equal(t, edit, updated.Content)
				require.Equal(t, original, updated.SourceContent)
				require.Equal(t, revision+1, updated.ContentRevision)
				require.Equal(t, "ready", updated.IndexStatus)
				stored, err := repo.GetChunkByID(ctx, k.TenantID, first.ID)
				require.NoError(t, err)
				require.Equal(t, original, stored.SourceContent)

				var evidence []types.KnowledgeEvidence
				require.NoError(t, db.Where("knowledge_id = ?", k.ID).Find(&evidence).Error)
				if revision == 0 {
					require.Empty(t, evidence, "editing a chunk must not create evidence")
				} else {
					require.Equal(t, []types.KnowledgeEvidence{firstEvidence}, evidence, "editing must leave existing evidence unchanged")
				}
				require.Zero(t, tasks.calls, "editing this source without summary enrichment must not enqueue extraction")

				require.NoError(t, source.snapshotContextSource(ctx, k))
				require.NoError(t, db.Where("knowledge_id = ?", k.ID).Find(&evidence).Error)
				require.Len(t, evidence, 1)
				require.Equal(t, original+"\n\n"+ending, evidence[0].Body)
				require.NotContains(t, evidence[0].Body, edit)
				if revision == 0 {
					firstEvidence = evidence[0]
				} else {
					require.Equal(t, firstEvidence.ID, evidence[0].ID, "unchanged source must reuse the evidence version")
				}
			}
		})
	}
}

type contextSourceChunks struct {
	interfaces.ChunkService
	items []*types.Chunk
}

func (s contextSourceChunks) ListChunksByKnowledgeID(context.Context, string) ([]*types.Chunk, error) {
	return s.items, nil
}

func TestContextSnapshotFromUneditedChunksPreservesParserOutput(t *testing.T) {
	chunks := contextSourceChunks{items: []*types.Chunk{
		{ChunkIndex: 3, ChunkType: types.ChunkTypeText, Content: "  Last paragraph.\n"},
		{ChunkIndex: 1, ChunkType: types.ChunkTypeSummary, Content: "Generated summary."},
		{ChunkIndex: 0, ChunkType: types.ChunkTypeText, Content: "First **parser** paragraph."},
		{ChunkIndex: 2, ChunkType: types.ChunkTypeImageOCR, Content: "Generated OCR."},
	}}
	body, err := contextBodyFromChunks(context.Background(), chunks, "source-message")
	require.NoError(t, err)
	require.Equal(t, "First **parser** paragraph.\n\n  Last paragraph.\n", body)
	for _, chunk := range chunks.items {
		require.Empty(t, chunk.SourceContent)
	}

	db := knowledgeContextTestDB(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	k := &types.Knowledge{ID: "source-message", TenantID: 9, KnowledgeBaseID: "kb", Type: "file",
		Title: "Unedited source", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, db.Create(k).Error)
	source := &knowledgeService{db: db, chunkService: chunks}
	require.NoError(t, source.snapshotContextSource(ctx, k))
	var evidence []types.KnowledgeEvidence
	require.NoError(t, db.Where("knowledge_id = ?", k.ID).Find(&evidence).Error)
	require.Len(t, evidence, 1)
	require.Equal(t, body, evidence[0].Body)
}
