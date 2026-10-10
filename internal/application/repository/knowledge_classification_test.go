package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestKnowledgeContentTypeFilterIncludesUnclassifiedAndRespectsScope(t *testing.T) {
	db := pgtest.Open(t)
	kbID := uuid.NewString()
	for _, value := range []string{types.ContentTypeEvent, types.ContentTypeTutorial, ""} {
		require.NoError(t, db.Create(&types.Knowledge{TenantID: 1, KnowledgeBaseID: kbID, Type: "file", Title: "Article", Source: "manual", ParseStatus: types.ParseStatusCompleted, ContentType: value}).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO knowledges (tenant_id, knowledge_base_id, type, title, source, parse_status, content_type) VALUES (1,?, 'file','Unclassified','manual','completed',NULL)`, kbID).Error)
	require.NoError(t, db.Create(&types.Knowledge{TenantID: 2, KnowledgeBaseID: kbID, Type: "file", Title: "Other space", Source: "manual", ParseStatus: types.ParseStatusCompleted, ContentType: types.ContentTypeEvent}).Error)
	repo := NewKnowledgeRepository(db)
	page := &types.Pagination{Page: 1, PageSize: 10}
	rows, total, err := repo.ListPagedKnowledgeByKnowledgeBaseID(context.Background(), 1, kbID, page, types.KnowledgeListFilter{ContentType: types.ContentTypeEvent})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	require.Equal(t, types.ContentTypeEvent, rows[0].ContentType)
	rows, total, err = repo.ListPagedKnowledgeByKnowledgeBaseID(context.Background(), 1, kbID, page, types.KnowledgeListFilter{ContentType: "unclassified"})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Empty(t, row.ContentType)
	}
}

func TestDataSourceDefaultContentTypeCanBeCleared(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewDataSourceRepository(db)
	ctx := context.Background()
	source := &types.DataSource{TenantID: types.SingleTenantID, KnowledgeBaseID: uuid.NewString(), Name: "RSS", Type: types.ConnectorTypeRSS, DefaultContentType: types.ContentTypeResearch}
	require.NoError(t, repo.Create(ctx, source))
	loaded, err := repo.FindByID(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContentTypeResearch, loaded.DefaultContentType)
	source.DefaultContentType = ""
	require.NoError(t, repo.Update(ctx, source))
	loaded, err = repo.FindByID(ctx, source.ID)
	require.NoError(t, err)
	require.Empty(t, loaded.DefaultContentType)
}
