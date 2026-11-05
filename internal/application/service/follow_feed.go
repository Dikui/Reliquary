package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *FollowService) eligibleFeed(ctx context.Context, spaceID uint64, cutoff time.Time, targetIDs []string, libraries ...string) *gorm.DB {
	query := s.db.WithContext(ctx).Table("entries k").Joins("JOIN libraries feed_library ON feed_library.id=k.library_id AND feed_library.deleted_at IS NULL AND feed_library.feed_enabled=true").Where("k.space_id=? AND k.created_at<=? AND COALESCE(k.follow_ready_at,k.processed_at,k.created_at)<=?", spaceID, cutoff, cutoff).Where(followEligibleSQL)
	if len(libraries) > 0 {
		query = query.Where("k.library_id IN ?", libraries)
	}
	query = query.Joins("LEFT JOIN follow_feed_library_states checkpoint ON checkpoint.space_id=k.space_id AND checkpoint.library_id=k.library_id")
	query = query.Joins(`JOIN LATERAL (
 SELECT MAX(matches.entered_at) AS entered_at, COUNT(*) AS match_count, ARRAY_AGG(matches.target_id) AS target_ids FROM (
 SELECT GREATEST(COALESCE(k.follow_ready_at,k.processed_at,k.created_at),ke.created_at) AS entered_at, f.target_id
 FROM entry_entities ke JOIN entities scoped_entity ON scoped_entity.id=ke.entity_id AND scoped_entity.space_id=ke.space_id JOIN follows f ON f.space_id=ke.space_id AND f.target_type='entity' AND f.target_id=ke.entity_id
 WHERE ke.excluded=false AND ke.space_id=k.space_id AND ke.entry_id=k.id AND ke.created_at<=? AND f.created_at<=?
 UNION ALL
 SELECT CASE WHEN kt.is_historical THEN NULL ELSE GREATEST(COALESCE(k.follow_ready_at,k.processed_at,k.created_at),kt.created_at) END, f.target_id
 FROM entry_topics kt JOIN topics t ON t.id=kt.topic_id AND t.space_id=kt.space_id AND t.library_id=k.library_id JOIN follows f ON f.space_id=kt.space_id AND f.target_type='topic' AND f.target_id=kt.topic_id
 WHERE kt.space_id=k.space_id AND kt.entry_id=k.id AND kt.created_at<=? AND f.created_at<=?
 ) matches) entry ON entry.match_count>0`, cutoff, cutoff, cutoff, cutoff)
	if len(targetIDs) > 0 {
		query = query.Where("EXISTS (SELECT 1 FROM unnest(entry.target_ids) target(id) WHERE target.id IN ?)", targetIDs)
	}
	return query

}

// feedNow uses the clock that stamps follow_ready_at in PostgreSQL.
func (s *FollowService) feedNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := s.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now.UTC(), err
}
func (s *FollowService) feedLibraries(ctx context.Context, spaceID uint64, selectors []string) ([]types.Library, error) {
	if len(selectors) > 100 {
		return nil, werrors.NewBadRequestError("libraries accepts at most 100 values")
	}
	var libraries []types.Library
	q := s.db.WithContext(ctx).Where("space_id=?", spaceID)
	if len(selectors) > 0 {
		q = q.Where("id IN ? OR directory IN ?", selectors, selectors)
	}
	if err := q.Order("directory,id").Find(&libraries).Error; err != nil {
		return nil, err
	}
	for _, selector := range selectors {
		found := false
		for _, lib := range libraries {
			if selector == lib.ID || selector == lib.Directory {
				found = true
			}
		}
		if !found {
			return nil, werrors.NewBadRequestError("unknown library: " + selector)
		}
	}
	enabled := []types.Library{}
	for _, lib := range libraries {
		if lib.FeedEnabled {
			enabled = append(enabled, lib)
		}
	}
	return enabled, nil
}
func (s *FollowService) readState(ctx context.Context, spaceID uint64, cutoff time.Time, selectors ...string) (*types.FollowFeedState, error) {
	libraries, err := s.feedLibraries(ctx, spaceID, selectors)
	if err != nil {
		return nil, err
	}
	state := &types.FollowFeedState{Libraries: []types.FollowLibraryState{}}
	for _, library := range libraries {
		item := types.FollowLibraryState{LibraryID: library.ID, LibraryName: library.Name, DisplayName: library.DisplayName(types.LanguageFromContextOrDefault(ctx))}
		var rows []types.FollowLibraryCheckpoint
		if err := s.db.WithContext(ctx).Where("space_id=? AND library_id=?", spaceID, library.ID).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			item.LastViewedAt = rows[0].LastViewedAt
		}
		q := s.eligibleFeed(ctx, spaceID, cutoff, nil, library.ID).Where("entry.entered_at IS NOT NULL AND (checkpoint.last_viewed_at IS NULL OR entry.entered_at>checkpoint.last_viewed_at)")
		if err := q.Count(&item.NewCount).Error; err != nil {
			return nil, err
		}
		state.NewCount += item.NewCount
		state.Libraries = append(state.Libraries, item)
	}
	// The scalar is only a compatibility projection. Eligibility always uses each library's checkpoint.
	if len(state.Libraries) > 0 {
		state.LastViewedAt = state.Libraries[0].LastViewedAt
	}
	for _, item := range state.Libraries {
		if item.LastViewedAt == nil {
			state.LastViewedAt = nil
			break
		}
		if state.LastViewedAt != nil && item.LastViewedAt.Before(*state.LastViewedAt) {
			state.LastViewedAt = item.LastViewedAt
		}
	}
	return state, nil
}
func (s *FollowService) State(ctx context.Context, selectors ...string) (*types.FollowFeedState, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	now, err := s.feedNow(ctx)
	if err != nil {
		return nil, err
	}
	return s.readState(ctx, spaceID, now, selectors...)
}

// MarkViewed advances only enabled libraries in the requested scope, monotonically.
func (s *FollowService) MarkViewed(ctx context.Context, viewedAt *time.Time, selectors ...string) (*types.FollowFeedState, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	now, err := s.feedNow(ctx)
	if err != nil {
		return nil, err
	}
	value := now
	if viewedAt != nil {
		value = viewedAt.UTC().Truncate(time.Microsecond)
		if value.After(now) || value.IsZero() {
			return nil, werrors.NewBadRequestError("viewed_at must not be in the future or zero")
		}
	}
	libraries, err := s.feedLibraries(ctx, spaceID, selectors)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, library := range libraries {
			if err := tx.Exec(`INSERT INTO follow_feed_library_states(space_id,library_id,last_viewed_at) VALUES(?,?,?) ON CONFLICT(space_id,library_id) DO UPDATE SET last_viewed_at=GREATEST(follow_feed_library_states.last_viewed_at,EXCLUDED.last_viewed_at)`, spaceID, library.ID, value).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.readState(ctx, spaceID, now, selectors...)
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
	Checkpoints map[string]*time.Time `json:"checkpoints"`
	EnteredAt   *time.Time            `json:"entered_at"`
	Version     int                   `json:"v"`
	SpaceID     uint64                `json:"space"`
	Cutoff      time.Time             `json:"cutoff"`
	Since       *time.Time            `json:"since"`
	Libraries   []string              `json:"libraries"`
	Targets     []string              `json:"targets"`
	Collapse    bool                  `json:"collapse"`
	IncludeSeen bool                  `json:"include_seen"`
	CollectedAt time.Time             `json:"collected"`
	EntryID     string                `json:"id"`
}

func decodeFollowCursor(value string, spaceID uint64) (*followCursor, error) {
	invalid := func() (*followCursor, error) { return nil, werrors.NewBadRequestError("invalid follow_feed cursor") }
	if len(value) > 8192 {
		return invalid()
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return invalid()
	}
	var cursor followCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.SpaceID != spaceID || cursor.Cutoff.IsZero() || cursor.CollectedAt.IsZero() || cursor.CollectedAt.After(cursor.Cutoff) || cursor.EntryID == "" || len(cursor.EntryID) > 36 {
		return invalid()
	}
	if _, _, err := normalizeFollowFeed(types.FollowFeedRequest{TargetIDs: cursor.Targets}); err != nil {
		return invalid()
	}
	return &cursor, nil
}

// Feed is read-only, including when called by an external MCP client.
func (s *FollowService) Feed(ctx context.Context, request types.FollowFeedRequest) (*types.FollowFeedResponse, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	since, limit, err := normalizeFollowFeed(request)
	if err != nil {
		return nil, err
	}
	cutoff, err := s.feedNow(ctx)
	if err != nil {
		return nil, err
	}
	var cursor *followCursor
	if request.Cursor != "" {
		cursor, err = decodeFollowCursor(request.Cursor, spaceID)
		if err != nil {
			return nil, err
		}
		if cursor.Cutoff.After(cutoff) {
			return nil, werrors.NewBadRequestError("invalid follow_feed cursor")
		}
		if (len(request.Libraries) > 0 && !slices.Equal(request.Libraries, cursor.Libraries)) || (request.Since != "" && (cursor.Since == nil || !since.Equal(*cursor.Since))) || (len(request.TargetIDs) > 0 && !slices.Equal(request.TargetIDs, cursor.Targets)) || (request.CollapseSimilar != nil && *request.CollapseSimilar != cursor.Collapse) || (request.IncludeSeen && !cursor.IncludeSeen) {
			return nil, werrors.NewBadRequestError("cursor filters cannot change")
		}
		cutoff, since = cursor.Cutoff, cursor.Since
		request.Libraries = cursor.Libraries
		request.TargetIDs, request.CollapseSimilar, request.IncludeSeen = cursor.Targets, &cursor.Collapse, cursor.IncludeSeen
	}
	var response *types.FollowFeedResponse
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		copy := *s
		copy.db = tx
		var err error
		response, err = copy.feedSnapshot(ctx, spaceID, request, since, limit, cutoff, cursor)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return response, err
}

func (s *FollowService) feedSnapshot(ctx context.Context, spaceID uint64, request types.FollowFeedRequest, since *time.Time, limit int, cutoff time.Time, cursor *followCursor) (*types.FollowFeedResponse, error) {
	state, err := s.readState(ctx, spaceID, cutoff, request.Libraries...)
	if err != nil {
		return nil, err
	}
	response := &types.FollowFeedResponse{Results: []*types.FollowFeedArticle{}, Libraries: state.Libraries, NewCount: state.NewCount, LastViewedAt: state.LastViewedAt, ViewedAt: cutoff}

	ids := []string{}
	checkpoints := map[string]*time.Time{}
	for _, lib := range state.Libraries {
		ids = append(ids, lib.LibraryID)
		checkpoints[lib.LibraryID] = lib.LastViewedAt
	}
	if cursor != nil && cursor.Checkpoints != nil {
		checkpoints = cursor.Checkpoints
	}
	if len(ids) == 0 {
		return response, nil
	}
	query := s.eligibleFeed(ctx, spaceID, cutoff, request.TargetIDs, ids...)
	if since == nil && !request.IncludeSeen {
		conditions := []string{}
		args := []any{}
		for _, id := range ids {
			condition := "(k.library_id=? AND entry.entered_at IS NOT NULL"
			args = append(args, id)
			if stamp := checkpoints[id]; stamp != nil {
				condition += " AND entry.entered_at>?"
				args = append(args, *stamp)
			}
			conditions = append(conditions, condition+")")
		}
		query = query.Where("("+strings.Join(conditions, " OR ")+")", args...)
	}
	if since != nil {
		query = query.Where("entry.entered_at>?", *since)
	}
	type collectedArticle struct {
		ID        string
		CreatedAt time.Time
		EnteredAt *time.Time
	}
	var rows []collectedArticle
	if err := query.Select("k.id,k.created_at,entry.entered_at").Order("entry.entered_at DESC NULLS LAST,k.created_at DESC,k.id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	response.Total = int64(len(rows))
	if len(rows) == 0 {
		return response, nil
	}
	// Reuse the retrieval projection and folding without invoking ranking or models.
	search := &searchService{db: s.db}
	var space types.Space
	if err := s.db.WithContext(ctx).Select("id,retrieval_config").Where("id=?", spaceID).First(&space).Error; err != nil {
		return nil, err
	}
	config := space.RetrievalConfig
	hits := make([]entrySearchHit, 0, len(rows))
	collected := map[string]time.Time{}
	entered := map[string]*time.Time{}
	for _, row := range rows {
		hits = append(hits, entrySearchHit{EntryID: row.ID, MatchType: types.MatchTypeKeywords})
		collected[row.ID] = row.CreatedAt
		entered[row.ID] = row.EnteredAt
	}
	articles, err := search.mergeEntryHits(ctx, spaceID, [][]entrySearchHit{nil, nil, hits}, config)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(articles, func(a, b *types.EntrySearchResult) int {
		left, right := entered[a.EntryID], entered[b.EntryID]
		if left == nil && right != nil {
			return 1
		}
		if left != nil && right == nil {
			return -1
		}
		if left != nil && right != nil {
			if n := right.Compare(*left); n != 0 {
				return n
			}
		}
		if result := collected[b.EntryID].Compare(collected[a.EntryID]); result != 0 {
			return result
		}
		return strings.Compare(a.EntryID, b.EntryID)
	})
	follows, err := s.ListFollows(ctx)
	if err != nil {
		return nil, err
	}
	var topics []types.Topic
	err = s.db.WithContext(ctx).Where("space_id=?", spaceID).Find(&topics).Error
	if err != nil {
		return nil, err
	}
	scopes := map[string]string{}
	for _, topic := range topics {
		scopes[topic.ID] = topic.LibraryID
	}
	var entities []types.Entity
	if err := s.db.WithContext(ctx).Select("id,library_id").Where("space_id=? AND merged_into_id=''", spaceID).Find(&entities).Error; err != nil {
		return nil, err
	}
	entityLibraries := map[string]string{}
	for _, entity := range entities {
		entityLibraries[entity.ID] = entity.LibraryID
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
			if follow, ok := byTarget[types.FollowTargetEntity+":"+entity.ID]; ok && entityLibraries[entity.ID] == article.LibraryID {
				matched[article.EntryID] = append(matched[article.EntryID], follow)
			}
		}
		for _, topic := range article.Topics {
			if follow, ok := byTarget[types.FollowTargetTopic+":"+topic.ID]; ok && scopes[topic.ID] == article.LibraryID {
				matched[article.EntryID] = append(matched[article.EntryID], follow)
			}
		}
	}
	if request.CollapseSimilar == nil || *request.CollapseSimilar {
		articles, err = search.collapseArticleReports(ctx, spaceID, articles, config)
		if err != nil {
			return nil, err
		}
	}
	var project func(*types.EntrySearchResult) *types.FollowFeedArticle
	project = func(article *types.EntrySearchResult) *types.FollowFeedArticle {
		date := collected[article.EntryID]
		result := &types.FollowFeedArticle{EntrySearchResult: *article, CollectedAt: date, EnteredAt: entered[article.EntryID],
			IsNew: entered[article.EntryID] != nil && (checkpoints[article.LibraryID] == nil || entered[article.EntryID].After(*checkpoints[article.LibraryID])), MatchedFollows: matched[article.EntryID], SimilarReports: []*types.FollowFeedArticle{}}
		for _, child := range article.SimilarReports {
			result.SimilarReports = append(result.SimilarReports, project(child))
		}
		return result
	}
	// Fold the full query window before keyset slicing so children cannot reappear.
	if cursor != nil {
		articles = slices.DeleteFunc(articles, func(a *types.EntrySearchResult) bool {
			stamp := entered[a.EntryID]
			if stamp != nil && cursor.EnteredAt == nil {
				return true
			}
			if stamp == nil && cursor.EnteredAt != nil {
				return false
			}
			if stamp != nil && cursor.EnteredAt != nil {
				if !stamp.Equal(*cursor.EnteredAt) {
					return stamp.After(*cursor.EnteredAt)
				}
			}
			date := collected[a.EntryID]
			return date.After(cursor.CollectedAt) || (date.Equal(cursor.CollectedAt) && a.EntryID <= cursor.EntryID)
		})
	}
	pageSize := min(limit, len(articles))
	for _, article := range articles[:pageSize] {
		response.Results = append(response.Results, project(article))
	}
	if len(articles) > pageSize {
		last := articles[pageSize-1]
		next := followCursor{Checkpoints: checkpoints, EnteredAt: entered[last.EntryID], Version: 1, SpaceID: spaceID, Cutoff: cutoff, Since: since, Targets: request.TargetIDs, Libraries: request.Libraries, Collapse: request.CollapseSimilar == nil || *request.CollapseSimilar, IncludeSeen: request.IncludeSeen, CollectedAt: collected[last.EntryID], EntryID: last.EntryID}
		data, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		response.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return response, nil
}
