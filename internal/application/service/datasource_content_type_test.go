package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/datasource"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestDataSourceRejectsUnsupportedDefaultContentType(t *testing.T) {
	svc := &DataSourceService{}
	for _, value := range []string{"unknown", "RESEARCH"} {
		_, err := svc.CreateDataSource(context.Background(), &types.DataSource{DefaultContentType: value})
		require.ErrorContains(t, err, "default_content_type")
		_, err = svc.UpdateDataSource(context.Background(), &types.DataSource{ID: "source", DefaultContentType: value})
		require.ErrorContains(t, err, "default_content_type")
	}
}

func TestDataSourceNormalizesDefaultContentTypeBeforePersistence(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Publisher</title><link>https://example.com/</link><description>Fixture feed</description></channel></rss>`))
	}))
	t.Cleanup(server.Close)
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	repo := repository.NewDataSourceRepository(db)
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(rss.NewConnector()))
	scheduler := datasource.NewScheduler(repo, nil, nil)
	t.Cleanup(scheduler.Stop)
	svc := &DataSourceService{dsRepo: repo, connectorRegistry: registry, scheduler: scheduler,
		kbService: &processSyncKBService{kb: &types.KnowledgeBase{ID: "kb-default-type", SpaceID: 1}}}
	config, err := (&types.DataSourceConfig{Type: types.ConnectorTypeRSS,
		Settings: map[string]interface{}{"feed_urls": server.URL + "/feed.xml", "publisher": "Publisher"}}).ToJSON()
	require.NoError(t, err)
	created, err := svc.CreateDataSource(ctx, &types.DataSource{SpaceID: 1, Name: "RSS", Type: types.ConnectorTypeRSS,
		Config: config, DefaultContentType: " research "})
	require.NoError(t, err)
	require.Equal(t, types.ContentTypeResearch, created.DefaultContentType)
	loaded, err := repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContentTypeResearch, loaded.DefaultContentType)
	created.DefaultContentType = " tutorial "
	updated, err := svc.UpdateDataSource(ctx, created)
	require.NoError(t, err)
	require.Equal(t, types.ContentTypeTutorial, updated.DefaultContentType)
	loaded, err = repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContentTypeTutorial, loaded.DefaultContentType)
}
