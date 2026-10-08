package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func chunkWikiFixture(t *testing.T, db *gorm.DB) (*types.Chunk, *types.WikiPage) {
	t.Helper()
	now := time.Now().UTC()
	chunk := &types.Chunk{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "chunk-wiki-kb", KnowledgeID: "document",
		Content: "Source body", SourceContent: "Source body", ChunkType: types.ChunkTypeText,
		IsEnabled: true, IndexStatus: "ready", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, NewChunkRepository(db).CreateChunks(context.Background(), []*types.Chunk{chunk}))
	page := &types.WikiPage{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: chunk.KnowledgeBaseID, Slug: "concept/" + uuid.NewString(),
		Title: "Referenced page", Content: "Wiki body", PageType: types.WikiPageTypeConcept,
		Status: types.WikiPageStatusPublished, ChunkRefs: types.StringArray{chunk.ID}, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, NewWikiPageRepository(db).Create(context.Background(), page))
	return chunk, page
}

func TestChunkRepositoryUpdatesMarkOnlyWikiSourceChanges(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	for _, method := range []string{"UpdateChunk", "SaveChunks", "UpdateChunks"} {
		t.Run(method, func(t *testing.T) {
			chunk, page := chunkWikiFixture(t, db)
			repo := NewChunkRepository(db)
			update := func() error {
				switch method {
				case "SaveChunks":
					return repo.SaveChunks(ctx, []*types.Chunk{chunk})
				case "UpdateChunks":
					return repo.UpdateChunks(ctx, []*types.Chunk{chunk})
				default:
					return repo.UpdateChunk(ctx, chunk)
				}
			}
			chunk.IndexStatus = "failed"
			chunk.Flags = types.ChunkFlagRecommended
			require.NoError(t, update())
			stored, err := NewWikiPageRepository(db).GetByID(ctx, page.ID)
			require.NoError(t, err)
			require.Empty(t, stored.SourceChanges, "metadata changes must leave source warning clear")
			chunk.IsEnabled = false
			require.NoError(t, update())
			stored, err = NewWikiPageRepository(db).GetByID(ctx, page.ID)
			require.NoError(t, err)
			require.Contains(t, stored.SourceChanges, chunk.ID)
			firstMark := stored.SourceChanges[chunk.ID]
			require.NoError(t, update())
			stored, err = NewWikiPageRepository(db).GetByID(ctx, page.ID)
			require.NoError(t, err)
			require.Equal(t, firstMark, stored.SourceChanges[chunk.ID], "identical save must not refresh the modification time")
			chunk.Content = "Edited body"
			require.NoError(t, update())
			stored, err = NewWikiPageRepository(db).GetByID(ctx, page.ID)
			require.NoError(t, err)
			require.Len(t, stored.SourceChanges, 1)
			require.NotEqual(t, firstMark, stored.SourceChanges[chunk.ID])
		})
	}
}

func TestChunkSourceWarningFailureRollsBackEditAndRevision(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	chunk, page := chunkWikiFixture(t, db)
	require.NoError(t, db.Exec("ALTER TABLE wiki_pages ADD CONSTRAINT reject_source_changes CHECK (source_changes = '{}'::jsonb)").Error)
	snapshot := &types.ChunkRevision{
		ID: uuid.NewString(), TenantID: chunk.TenantID, KnowledgeBaseID: chunk.KnowledgeBaseID,
		KnowledgeID: chunk.KnowledgeID, ChunkID: chunk.ID, Revision: 0, Content: chunk.Content,
		IsEnabled: true, EditSource: "user", EditedAt: chunk.UpdatedAt, CreatedAt: time.Now(),
	}
	chunk.Content = "Must roll back"
	chunk.ContentRevision = 1
	chunk.UpdatedAt = time.Now()
	require.Error(t, NewChunkRepository(db).SaveChunkRevision(ctx, chunk, snapshot, 0))
	stored, err := NewChunkRepository(db).GetChunkByID(ctx, 1, chunk.ID)
	require.NoError(t, err)
	require.Equal(t, "Source body", stored.Content)
	require.Equal(t, 0, stored.ContentRevision)
	revisions, err := NewChunkRepository(db).ListChunkRevisions(ctx, 1, chunk.ID)
	require.NoError(t, err)
	require.Empty(t, revisions)
	storedPage, err := NewWikiPageRepository(db).GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, storedPage.SourceChanges)
}

func TestChunkCreationDoesNotMarkWikiSource(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	chunk, page := chunkWikiFixture(t, db)
	require.NoError(t, NewChunkRepository(db).DeleteChunk(ctx, 1, chunk.ID))
	chunk.ID = uuid.NewString()
	require.NoError(t, db.Model(&types.WikiPage{}).Where("id = ?", page.ID).Update("chunk_refs", types.StringArray{chunk.ID}).Error)
	require.NoError(t, NewChunkRepository(db).UpdateChunk(ctx, chunk))
	stored, err := NewWikiPageRepository(db).GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, stored.SourceChanges)
}
