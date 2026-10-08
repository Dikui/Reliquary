package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func sourceChangeChunk(t *testing.T, db *gorm.DB, content string) *types.Chunk {
	t.Helper()
	now := time.Now().UTC()
	chunk := &types.Chunk{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "source-change-kb", KnowledgeID: "source-change-document",
		ChunkType: types.ChunkTypeText, Content: content, SourceContent: content,
		IsEnabled: true, IndexStatus: "ready", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repository.NewChunkRepository(db).CreateChunks(context.Background(), []*types.Chunk{chunk}))
	return chunk
}

func sourceChangePage(t *testing.T, db *gorm.DB, slug string, chunks ...string) *types.WikiPage {
	t.Helper()
	now := time.Now().UTC()
	page := &types.WikiPage{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "source-change-kb", Slug: slug,
		Title: slug, Content: "Existing Wiki content", PageType: types.WikiPageTypeConcept,
		Status: types.WikiPageStatusPublished, Version: 1, ChunkRefs: chunks,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repository.NewWikiPageRepository(db).Create(context.Background(), page))
	return page
}

func chunkSourceChangeService(db *gorm.DB) *chunkService {
	return &chunkService{
		chunkRepository: repository.NewChunkRepository(db),
		knowledgeRepo:   editableChunkKnowledgeRepo{}, kbRepository: editableChunkKBRepo{},
	}
}

func sourceChangeContext() context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
}

func TestDocumentChunkEditMarksReferencedWikiPagesAndRevert(t *testing.T) {
	db := pgtest.Open(t)
	chunk := sourceChangeChunk(t, db, "Immutable source")
	other := sourceChangeChunk(t, db, "Unrelated source")
	page := sourceChangePage(t, db, "concept/referenced", chunk.ID)
	unrelated := sourceChangePage(t, db, "concept/unrelated", other.ID)
	repo := repository.NewWikiPageRepository(db)
	svc := chunkSourceChangeService(db)
	ctx := sourceChangeContext()
	content := "Edited working copy"
	updated, err := svc.UpdateDocumentChunk(ctx, chunk.ID, &content, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Immutable source", updated.SourceContent)
	marked, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Len(t, marked.SourceChanges, 1)
	require.Contains(t, marked.SourceChanges, chunk.ID)
	require.Equal(t, page.Content, marked.Content)
	require.Equal(t, page.Version, marked.Version)
	unchanged, err := repo.GetByID(ctx, unrelated.ID)
	require.NoError(t, err)
	require.Empty(t, unchanged.SourceChanges)

	// Ignore the first warning so reverting must produce a fresh one.
	require.NoError(t, db.Model(&types.WikiPage{}).Where("id = ?", page.ID).Update("source_changes", gorm.Expr("'{}'::jsonb")).Error)
	updated, err = svc.RevertDocumentChunk(ctx, chunk.ID, 0, nil)
	require.NoError(t, err)
	require.Equal(t, "Immutable source", updated.Content)
	require.Equal(t, "Immutable source", updated.SourceContent)
	require.Equal(t, 2, updated.ContentRevision)
	marked, err = repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Contains(t, marked.SourceChanges, chunk.ID)
}

func TestDocumentChunkEnabledChangesMergeWikiSourceRecords(t *testing.T) {
	db := pgtest.Open(t)
	first := sourceChangeChunk(t, db, "First source")
	second := sourceChangeChunk(t, db, "Second source")
	page := sourceChangePage(t, db, "concept/shared", first.ID, second.ID)
	repo := repository.NewWikiPageRepository(db)
	svc := chunkSourceChangeService(db)
	ctx := sourceChangeContext()
	before := time.Now().UTC()
	disabled := false
	_, err := svc.UpdateDocumentChunk(ctx, first.ID, nil, &disabled, nil)
	require.NoError(t, err)
	firstMark, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	firstTime := firstMark.SourceChanges[first.ID]
	require.False(t, firstTime.IsZero())
	require.False(t, firstTime.Before(before))

	content := "Second source edited"
	_, err = svc.UpdateDocumentChunk(ctx, second.ID, &content, nil, nil)
	require.NoError(t, err)
	enabled := true
	_, err = svc.UpdateDocumentChunk(ctx, first.ID, nil, &enabled, nil)
	require.NoError(t, err)
	merged, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Len(t, merged.SourceChanges, 2)
	latestTime := merged.SourceChanges[first.ID]
	require.False(t, latestTime.Before(firstTime))
	require.Contains(t, merged.SourceChanges, second.ID)
}

func TestDocumentChunkNoopAndConflictDoNotMarkWiki(t *testing.T) {
	db := pgtest.Open(t)
	chunk := sourceChangeChunk(t, db, "Original body")
	page := sourceChangePage(t, db, "concept/noop", chunk.ID)
	svc := chunkSourceChangeService(db)
	ctx := sourceChangeContext()
	_, err := svc.UpdateDocumentChunk(ctx, chunk.ID, &chunk.Content, &chunk.IsEnabled, nil)
	require.NoError(t, err)
	wrongRevision := 3
	content := "Rejected edit"
	_, err = svc.UpdateDocumentChunk(ctx, chunk.ID, &content, nil, &wrongRevision)
	require.ErrorIs(t, err, ErrChunkRevisionConflict)
	stored, err := repository.NewWikiPageRepository(db).GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, stored.SourceChanges)
}

type wikiSourceFailingIndexKBRepo struct {
	interfaces.KnowledgeBaseRepository
}

func (wikiSourceFailingIndexKBRepo) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, errors.New("index unavailable")
}

func TestDocumentChunkIndexFailureStillMarksSavedWikiSource(t *testing.T) {
	db := pgtest.Open(t)
	chunk := sourceChangeChunk(t, db, "Original body")
	page := sourceChangePage(t, db, "concept/index-failed", chunk.ID)
	svc := chunkSourceChangeService(db)
	svc.kbRepository = wikiSourceFailingIndexKBRepo{}
	content := "Accepted edit"
	updated, err := svc.UpdateDocumentChunk(sourceChangeContext(), chunk.ID, &content, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "failed", updated.IndexStatus)
	stored, err := repository.NewWikiPageRepository(db).GetByID(sourceChangeContext(), page.ID)
	require.NoError(t, err)
	require.Contains(t, stored.SourceChanges, chunk.ID)
}

func TestDocumentChunkParentRebuildMarksParentWikiReferences(t *testing.T) {
	db := pgtest.Open(t)
	parent := sourceChangeChunk(t, db, "Original body")
	parent.ChunkType = types.ChunkTypeParentText
	parent.EndAt = len([]rune(parent.Content))
	require.NoError(t, repository.NewChunkRepository(db).UpdateChunk(context.Background(), parent))
	child := sourceChangeChunk(t, db, "Original body")
	child.ParentChunkID = parent.ID
	child.EndAt = parent.EndAt
	require.NoError(t, repository.NewChunkRepository(db).UpdateChunk(context.Background(), child))
	page := sourceChangePage(t, db, "concept/parent", parent.ID)
	content := "Edited child"
	_, err := chunkSourceChangeService(db).UpdateDocumentChunk(sourceChangeContext(), child.ID, &content, nil, nil)
	require.NoError(t, err)
	stored, err := repository.NewWikiPageRepository(db).GetByID(sourceChangeContext(), page.ID)
	require.NoError(t, err)
	require.Contains(t, stored.SourceChanges, parent.ID)
}

func TestDocumentChunkImageRemovalMarksImageWikiReferences(t *testing.T) {
	db := pgtest.Open(t)
	parent := sourceChangeChunk(t, db, "Image source\n![image](resource://one)")
	image := sourceChangeChunk(t, db, "OCR body")
	image.ChunkType = types.ChunkTypeImageOCR
	image.ParentChunkID = parent.ID
	info, err := json.Marshal([]types.ImageInfo{{URL: "resource://one"}})
	require.NoError(t, err)
	image.ImageInfo = string(info)
	require.NoError(t, repository.NewChunkRepository(db).UpdateChunk(context.Background(), image))
	page := sourceChangePage(t, db, "concept/image", image.ID)
	content := "Image source"
	_, err = chunkSourceChangeService(db).UpdateDocumentChunk(sourceChangeContext(), parent.ID, &content, nil, nil)
	require.NoError(t, err)
	stored, err := repository.NewWikiPageRepository(db).GetByID(sourceChangeContext(), page.ID)
	require.NoError(t, err)
	require.Contains(t, stored.SourceChanges, image.ID)
	image, err = repository.NewChunkRepository(db).GetChunkByID(sourceChangeContext(), 1, image.ID)
	require.NoError(t, err)
	require.False(t, image.IsEnabled)
}
