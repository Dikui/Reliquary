package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *FollowService) eligibleFeed(ctx context.Context, tenantID uint64, cutoff time.Time, targetIDs []string) *gorm.DB {
	query := s.db.WithContext(ctx).Table("knowledges k").Where("k.tenant_id=? AND k.created_at<=? AND k.follow_ready_at<=?", tenantID, cutoff, cutoff).Where(followEligibleSQL)
	query = query.Joins(`JOIN LATERAL (
 SELECT MAX(matches.entered_at) AS entered_at, COUNT(*) AS match_count, ARRAY_AGG(matches.target_id) AS target_ids FROM (
 SELECT GREATEST(k.follow_ready_at,ke.created_at) AS entered_at, f.target_id
 FROM knowledge_entities ke JOIN follows f ON f.tenant_id=ke.tenant_id AND f.target_type='entity' AND f.target_id=ke.entity_id
 WHERE ke.tenant_id=k.tenant_id AND ke.knowledge_id=k.id AND ke.created_at<=? AND f.created_at<=?
 UNION ALL
 SELECT CASE WHEN kt.is_historical THEN NULL ELSE GREATEST(k.follow_ready_at,kt.created_at) END, f.target_id
 FROM knowledge_topics kt JOIN follows f ON f.tenant_id=kt.tenant_id AND f.target_type='topic' AND f.target_id=kt.topic_id
 WHERE kt.tenant_id=k.tenant_id AND kt.knowledge_id=k.id AND kt.created_at<=? AND f.created_at<=?
 ) matches) entry ON entry.match_count>0`, cutoff, cutoff, cutoff, cutoff)
	if len(targetIDs) > 0 {
		query = query.Where("EXISTS (SELECT 1 FROM unnest(entry.target_ids) target(id) WHERE target.id IN ?)", targetIDs)
	}
	return query

}

func (s *FollowService) readState(ctx context.Context, tenantID uint64, cutoff time.Time) (*types.FollowFeedState, error) {
	state := &types.FollowFeedState{}
	var checkpoint types.FollowFeedCheckpoint
	err := s.db.WithContext(ctx).Where("tenant_id=?", tenantID).First(&checkpoint).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	state.LastViewedAt = checkpoint.LastViewedAt
	query := s.eligibleFeed(ctx, tenantID, cutoff, nil).Where("entry.entered_at IS NOT NULL")
	if state.LastViewedAt != nil {
		query = query.Where("entry.entered_at>?", *state.LastViewedAt)
	}
	if err := query.Count(&state.NewCount).Error; err != nil {
		return nil, err
	}
	return state, nil
}

func (s *FollowService) State(ctx context.Context) (*types.FollowFeedState, error) {
	tenantID, err := followTenant(ctx)
	if err != nil {
		return nil, err
	}
	return s.readState(ctx, tenantID, time.Now().UTC())
}

// MarkViewed advances monotonically. A page can acknowledge its captured cutoff.
func (s *FollowService) MarkViewed(ctx context.Context, viewedAt *time.Time) (*types.FollowFeedState, error) {
	tenantID, err := followTenant(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	value := now
	if viewedAt != nil {
		value = viewedAt.UTC().Truncate(time.Microsecond)
		if value.After(now) || value.IsZero() {
			return nil, werrors.NewBadRequestError("viewed_at must not be in the future or zero")
		}
	}
	if err := s.db.WithContext(ctx).Exec(`INSERT INTO follow_feed_states (tenant_id,last_viewed_at) VALUES (?,?)
        ON CONFLICT (tenant_id) DO UPDATE SET last_viewed_at=GREATEST(follow_feed_states.last_viewed_at,EXCLUDED.last_viewed_at)`, tenantID, value).Error; err != nil {
		return nil, err
	}
	return s.readState(ctx, tenantID, now)
}

func normalizeFollowFeed(request types.FollowFeedRequest) (*time.Time, int, error) {
	if request.Limit < 0 || request.Limit > 100 {
		return nil, 0, werrors.NewBadRequestError("limit must be between 1 and 100, or omitted")
	}
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	if len(request.TargetIDs) > 100 {
		return nil, 0, werrors.NewBadRequestError("target_ids accepts at most 100 IDs")
	}
	for _, id := range request.TargetIDs {
		if strings.TrimSpace(id) == "" || len(id) > 36 {
			return nil, 0, werrors.NewBadRequestError("target_ids requires nonempty IDs of at most 36 characters")
		}
	}
	if request.Since == "" {
		return nil, limit, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
		if value, err := time.Parse(layout, request.Since); err == nil {
			return &value, limit, nil
		}
	}
	return nil, 0, werrors.NewBadRequestError("since must be RFC3339 or YYYY-MM-DD")
}

type followCursor struct {
	Version     int        `json:"v"`
	TenantID    uint64     `json:"tenant"`
	Cutoff      time.Time  `json:"cutoff"`
	Since       *time.Time `json:"since"`
	Targets     []string   `json:"targets"`
	Collapse    bool       `json:"collapse"`
	IncludeSeen bool       `json:"include_seen"`
	CollectedAt time.Time  `json:"collected"`
	ArticleID   string     `json:"id"`
}

func decodeFollowCursor(value string, tenantID uint64) (*followCursor, error) {
	invalid := func() (*followCursor, error) { return nil, werrors.NewBadRequestError("invalid follow_feed cursor") }
	if len(value) > 8192 {
		return invalid()
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return invalid()
	}
	var cursor followCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.TenantID != tenantID || cursor.Cutoff.IsZero() || cursor.Cutoff.After(time.Now()) || cursor.CollectedAt.IsZero() || cursor.CollectedAt.After(cursor.Cutoff) || cursor.ArticleID == "" || len(cursor.ArticleID) > 36 {
		return invalid()
	}
	if _, _, err := normalizeFollowFeed(types.FollowFeedRequest{TargetIDs: cursor.Targets}); err != nil {
		return invalid()
	}
	return &cursor, nil
}

// Feed is read-only, including when called by an external MCP client.
func (s *FollowService) Feed(ctx context.Context, request types.FollowFeedRequest) (*types.FollowFeedResponse, error) {
	tenantID, err := followTenant(ctx)
	if err != nil {
		return nil, err
	}
	since, limit, err := normalizeFollowFeed(request)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	var cursor *followCursor
	if request.Cursor != "" {
		cursor, err = decodeFollowCursor(request.Cursor, tenantID)
		if err != nil {
			return nil, err
		}
		if (request.Since != "" && (cursor.Since == nil || !since.Equal(*cursor.Since))) || (len(request.TargetIDs) > 0 && !slices.Equal(request.TargetIDs, cursor.Targets)) || (request.CollapseSimilar != nil && *request.CollapseSimilar != cursor.Collapse) || (request.IncludeSeen && !cursor.IncludeSeen) {
			return nil, werrors.NewBadRequestError("cursor filters cannot change")
		}
		cutoff, since = cursor.Cutoff, cursor.Since
		request.TargetIDs, request.CollapseSimilar, request.IncludeSeen = cursor.Targets, &cursor.Collapse, cursor.IncludeSeen
	}
	var response *types.FollowFeedResponse
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		copy := *s
		copy.db = tx
		var err error
		response, err = copy.feedSnapshot(ctx, tenantID, request, since, limit, cutoff, cursor)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return response, err
}

func (s *FollowService) feedSnapshot(ctx context.Context, tenantID uint64, request types.FollowFeedRequest, since *time.Time, limit int, cutoff time.Time, cursor *followCursor) (*types.FollowFeedResponse, error) {
	state, err := s.readState(ctx, tenantID, cutoff)
	if err != nil {
		return nil, err
	}
	response := &types.FollowFeedResponse{Results: []*types.FollowFeedArticle{}, NewCount: state.NewCount, LastViewedAt: state.LastViewedAt, ViewedAt: cutoff}
	if cursor == nil && since == nil && !request.IncludeSeen {
		since = state.LastViewedAt
	}
	query := s.eligibleFeed(ctx, tenantID, cutoff, request.TargetIDs)
	if since != nil {
		query = query.Where("entry.entered_at>?", *since)
	}
	type collectedArticle struct {
		ID        string
		CreatedAt time.Time
		EnteredAt *time.Time
	}
	var rows []collectedArticle
	if err := query.Select("k.id,k.created_at,entry.entered_at").Order("k.created_at DESC,k.id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	response.Total = int64(len(rows))
	if len(rows) == 0 {
		return response, nil
	}
	// Reuse the retrieval projection and folding without invoking ranking or models.
	search := &knowledgeSearchService{db: s.db}
	var tenant types.Tenant
	if err := s.db.WithContext(ctx).Select("id,retrieval_config").Where("id=?", tenantID).First(&tenant).Error; err != nil {
		return nil, err
	}
	config := tenant.RetrievalConfig
	hits := make([]articleSearchHit, 0, len(rows))
	collected := map[string]time.Time{}
	entered := map[string]*time.Time{}
	for _, row := range rows {
		hits = append(hits, articleSearchHit{KnowledgeID: row.ID, MatchType: types.MatchTypeKeywords})
		collected[row.ID] = row.CreatedAt
		entered[row.ID] = row.EnteredAt
	}
	articles, err := search.mergeArticleHits(ctx, tenantID, [][]articleSearchHit{nil, nil, hits}, config)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(articles, func(a, b *types.ArticleSearchResult) int {
		if result := collected[b.ArticleID].Compare(collected[a.ArticleID]); result != 0 {
			return result
		}
		return strings.Compare(a.ArticleID, b.ArticleID)
	})
	follows, err := s.ListFollows(ctx)
	if err != nil {
		return nil, err
	}
	byTarget := map[string]types.FollowTarget{}
	for _, follow := range follows {
		if !follow.CreatedAt.After(cutoff) {
			byTarget[follow.TargetType+":"+follow.TargetID] = follow
		}
	}
	matched := map[string][]types.FollowTarget{}
	for _, article := range articles {
		article.Score = 0
		for i := range article.Snippets {
			article.Snippets[i].Score, article.Snippets[i].MatchType = 0, "summary"
		}
		for _, entity := range article.Entities {
			if follow, ok := byTarget[types.FollowTargetEntity+":"+entity.ID]; ok {
				matched[article.ArticleID] = append(matched[article.ArticleID], follow)
			}
		}
		for _, topic := range article.Topics {
			if follow, ok := byTarget[types.FollowTargetTopic+":"+topic.ID]; ok {
				matched[article.ArticleID] = append(matched[article.ArticleID], follow)
			}
		}
	}
	if request.CollapseSimilar == nil || *request.CollapseSimilar {
		articles, err = search.collapseArticleReports(ctx, tenantID, articles, config)
		if err != nil {
			return nil, err
		}
	}
	var project func(*types.ArticleSearchResult) *types.FollowFeedArticle
	project = func(article *types.ArticleSearchResult) *types.FollowFeedArticle {
		date := collected[article.ArticleID]
		result := &types.FollowFeedArticle{ArticleSearchResult: *article, CollectedAt: date, EnteredAt: entered[article.ArticleID],
			IsNew: entered[article.ArticleID] != nil && (state.LastViewedAt == nil || entered[article.ArticleID].After(*state.LastViewedAt)), MatchedFollows: matched[article.ArticleID], SimilarReports: []*types.FollowFeedArticle{}}
		for _, child := range article.SimilarReports {
			result.SimilarReports = append(result.SimilarReports, project(child))
		}
		return result
	}
	// Fold the full query window before keyset slicing so children cannot reappear.
	if cursor != nil {
		articles = slices.DeleteFunc(articles, func(a *types.ArticleSearchResult) bool {
			date := collected[a.ArticleID]
			return date.After(cursor.CollectedAt) || (date.Equal(cursor.CollectedAt) && a.ArticleID <= cursor.ArticleID)
		})
	}
	pageSize := min(limit, len(articles))
	for _, article := range articles[:pageSize] {
		response.Results = append(response.Results, project(article))
	}
	if len(articles) > pageSize {
		last := articles[pageSize-1]
		next := followCursor{Version: 1, TenantID: tenantID, Cutoff: cutoff, Since: since, Targets: request.TargetIDs, Collapse: request.CollapseSimilar == nil || *request.CollapseSimilar, IncludeSeen: request.IncludeSeen, CollectedAt: collected[last.ArticleID], ArticleID: last.ArticleID}
		data, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		response.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return response, nil
}
