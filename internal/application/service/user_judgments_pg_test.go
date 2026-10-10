package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/types"
)

func TestArticleStarPersistsHeaderLogsAndListFilter(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "starfixture", "# Starfixture\n\nOriginal measured deployment observations with evidence.")
	_, _, body := readVaultArticle(t, v, k)
	logs := NewOperationLogService(h.db)
	page, err := logs.List(h.ctx, "", 1, 100)
	require.NoError(t, err)
	require.Zero(t, page.Total, "automatic intake and processing do not log user judgments")
	for _, starred := range []bool{true, false} {
		updated, err := h.service.SetStarred(h.ctx, k.ID, starred)
		require.NoError(t, err)
		require.Equal(t, starred, updated.Starred)
		require.Eventually(t, func() bool {
			data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
			if err != nil {
				return false
			}
			header, got, err := ParseVaultArticle(data)
			return err == nil && header.Starred == starred && string(got) == string(body)
		}, time.Second, 10*time.Millisecond)
		_, total, err := repository.NewKnowledgeRepository(h.db).ListPagedKnowledgeByKnowledgeBaseID(h.ctx, h.kb.TenantID, h.kb.ID, &types.Pagination{Page: 1, PageSize: 20}, types.KnowledgeListFilter{Starred: &starred})
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		opposite := !starred
		_, total, err = repository.NewKnowledgeRepository(h.db).ListPagedKnowledgeByKnowledgeBaseID(h.ctx, h.kb.TenantID, h.kb.ID, &types.Pagination{Page: 1, PageSize: 20}, types.KnowledgeListFilter{Starred: &opposite})
		require.NoError(t, err)
		require.Zero(t, total)
	}
	page, err = logs.List(h.ctx, "", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Equal(t, []string{"unstar", "star"}, []string{page.Items[0].OperationType, page.Items[1].OperationType})
	require.JSONEq(t, `{"starred":true}`, string(page.Items[0].Before))
	require.JSONEq(t, `{"starred":false}`, string(page.Items[0].After))
	require.Equal(t, "web_user:personal-user", page.Items[0].Actor)
	_, err = h.service.SetStarred(h.ctx, k.ID, false)
	require.NoError(t, err)
	page, err = logs.List(h.ctx, "star", 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
}

func TestArticleSearchStarredFiltersAndMissingCount(t *testing.T) {
	h := newArticleSearchFixture(t)
	a := h.add(t, "starred", "article", "2026-10-10", "event", "semanticlaunch", 1)
	h.add(t, "plain", "article", "2026-10-10", "event", "semanticlaunch", 1)
	h.add(t, "mine", "mine", "", "", "semanticlaunch", 1)
	missing := h.add(t, "undated-star", "article", "", "research", "semanticlaunch", 1)
	require.NoError(t, h.db.Model(a).Update("starred", true).Error)
	require.NoError(t, h.db.Model(missing).Update("starred", true).Error)
	starred := true
	result, err := h.svc.Search(h.ctx, types.ArticleSearchRequest{Query: "semanticlaunch", Starred: &starred, CollapseSimilar: boolPointer(false), Limit: 100})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	for _, r := range result.Results {
		require.True(t, r.Starred)
	}
	result, err = h.svc.Search(h.ctx, types.ArticleSearchRequest{Query: "semanticlaunch", Starred: &starred, PublishedFrom: "2026-10-01", Limit: 100})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.EqualValues(t, 1, result.ExcludedMissingPublishedAt)
	starred = false
	result, err = h.svc.Search(h.ctx, types.ArticleSearchRequest{Query: "semanticlaunch", Starred: &starred, Limit: 100})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, "plain", result.Results[0].ArticleID)
}

func boolPointer(v bool) *bool { return &v }

func TestUndatedArticlePassesReviewStoresNullAndSearchExcludesIt(t *testing.T) {
	for _, date := range []string{"", "not a source date"} {
		t.Run(date, func(t *testing.T) {
			h := newRetainedIntakeHarness(t)
			v, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
			require.NoError(t, err)
			h.service.SetVault(v)
			t.Cleanup(func() { require.NoError(t, v.Close()) })
			a, err := h.service.CreateKnowledgeFromFile(h.ctx, h.kb.ID, newMultipartFileHeader(t, "undated.md", "# Undatedfixture\n\nAcme publishes original observations and complete measurements about product capabilities."), map[string]string{"publisher": "Acme", "original_url": "https://example.com/undatedfixture", "published_at": date}, nil, "", types.ChannelWeb, nil)
			require.NoError(t, err)
			h.drain(t)
			a = h.article(t, a.ID)
			h.kb.IndexingStrategy.KeywordEnabled = true
			require.NoError(t, h.db.Save(h.kb).Error)
			require.Equal(t, types.ParseStatusCompleted, a.ParseStatus)
			intake, err := a.IntakeMetadata()
			require.NoError(t, err)
			require.Equal(t, "approved", intake.Decision)
			require.Empty(t, intake.SourceInfo.PublishedAt)
			var row struct {
				PublishedAt *time.Time `gorm:"column:search_published_at"`
			}
			require.NoError(t, h.db.Table("knowledges").Select("search_published_at").Where("id=?", a.ID).Scan(&row).Error)
			require.Nil(t, row.PublishedAt)
			_, header, _ := readVaultArticle(t, v, a)
			require.Contains(t, string(header), "published_at: null\n")
			search := NewKnowledgeSearchService(articleSearchFixtureBases{kb: h.kb}, nil, nil, h.db)
			tenant, _ := types.TenantInfoFromContext(h.ctx)
			tenant.RetrievalConfig = &types.RetrievalConfig{KeywordThreshold: .00001, RerankTopK: 20}
			result, err := search.Search(h.ctx, types.ArticleSearchRequest{Query: "Undatedfixture", PublishedFrom: "2026-10-01", Limit: 20})
			require.NoError(t, err)
			require.Empty(t, result.Results)
			require.EqualValues(t, 1, result.ExcludedMissingPublishedAt)
			require.Contains(t, h.model.prompts[0], "must not cause rejection")
		})
	}
}

func TestReviewActionsAndEntityMergeRecordOnlyUserOperations(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	for _, action := range []string{"approve", "reject", "retry", "discard"} {
		a := &types.Knowledge{ID: "review-" + action, TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID, Type: "manual", Title: action, ParseStatus: types.ParseStatusRejected}
		path, err := h.service.fileSvc.SaveBytes(h.ctx, []byte("Original complete evidence."), h.kb.TenantID, a.ID+".md", false)
		require.NoError(t, err)
		require.NoError(t, a.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "rejected", Reason: "Needs human judgment", OriginalPath: path, SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.com/" + action}}))
		require.NoError(t, h.db.Create(a).Error)
		_, err = h.service.ReviewKnowledgeIntake(h.ctx, a.ID, action, types.SourceInfo{})
		require.NoError(t, err)
	}
	page, err := NewOperationLogService(h.db).List(h.ctx, "", 1, 100)
	require.NoError(t, err)
	require.Len(t, page.Items, 4)
	for _, action := range []string{"approve", "reject", "retry", "discard"} {
		page, err := NewOperationLogService(h.db).List(h.ctx, "review_"+action, 1, 100)
		require.NoError(t, err)
		require.EqualValues(t, 1, page.Total)
		var before map[string]any
		require.NoError(t, json.Unmarshal(page.Items[0].Before, &before))
		require.Equal(t, "rejected", before["decision"])
	}
	db, ctx, entities := entityFixture(t)
	keep, merged := entityRecord("keep-log", "Keep", "company"), entityRecord("merge-log", "Merged", "company")
	require.NoError(t, db.Create(&keep).Error)
	require.NoError(t, db.Create(&merged).Error)
	require.NoError(t, entities.MergeEntities(ctx, keep.ID, merged.ID))
	page, err = NewOperationLogService(db).List(ctx, "merge_entities", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.JSONEq(t, `{"keep_entity_id":"keep-log","merge_entity_id":"merge-log","merged_into_id":"keep-log"}`, string(page.Items[0].After))
	noActor := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	require.NoError(t, NewOperationLogService(db).Record(noActor, "star", "automatic", false, true))
	page, err = NewOperationLogService(db).List(ctx, "", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
}

func TestIntakeReviewAuditFailurePreservesDecisionAndOriginal(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	for _, action := range []string{"approve", "reject", "retry", "discard"} {
		a := &types.Knowledge{ID: "audit-failure-" + action, TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID, Type: "manual", Title: action, ParseStatus: types.ParseStatusRejected}
		path, err := h.service.fileSvc.SaveBytes(h.ctx, []byte("Original complete evidence."), h.kb.TenantID, a.ID, false)
		require.NoError(t, err)
		require.NoError(t, a.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "rejected", Reason: "Needs judgment", OriginalPath: path, SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.org/" + action}}))
		require.NoError(t, h.db.Create(a).Error)
	}
	require.NoError(t, h.db.Exec(`CREATE FUNCTION fail_review_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER fail_review_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION fail_review_audit()`).Error)
	for _, action := range []string{"approve", "reject", "retry", "discard"} {
		_, err := h.service.ReviewKnowledgeIntake(h.ctx, "audit-failure-"+action, action, types.SourceInfo{})
		require.ErrorContains(t, err, "audit unavailable")
		a := h.article(t, "audit-failure-"+action)
		intake, err := a.IntakeMetadata()
		require.NoError(t, err)
		require.Equal(t, "rejected", intake.Decision)
		require.Equal(t, types.ParseStatusRejected, a.ParseStatus)
		reader, err := h.service.fileSvc.GetFile(h.ctx, intake.OriginalPath)
		require.NoError(t, err)
		reader.Close()
	}
	require.Empty(t, h.queue.tasks)
}

func TestIntakePublicationNormalizationMatchesSearchFormat(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{" 2026-10-01 ", "2026-10-01"}, {"2026-10-01 08:00:00", "2026-10-01T08:00:00Z"},
		{"2026-10-01T08:00:00+08:00", "2026-10-01T00:00:00Z"}, {"unknown", ""},
	} {
		require.Equal(t, tt.want, normalizeIntakePublicationDate(tt.raw))
	}
}
