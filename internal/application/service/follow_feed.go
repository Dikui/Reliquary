package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"reliquary/internal/types"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
)

func (s *FollowService) feedLibraries(
	ctx context.Context,
	spaceID uint64,
	selectors []string,
) ([]types.Library, error) {
	if len(selectors) > 100 {
		return nil, werrors.NewBadRequestError("libraries accepts at most 100 values")
	}
	libraries := []types.Library{}
	q := s.db.WithContext(ctx).Where("space_id=?", spaceID)
	if len(selectors) > 0 {
		q = q.Where("id IN ? OR directory IN ?", selectors, selectors)
	}
	if err := q.Order("directory,id").Find(&libraries).Error; err != nil {
		return nil, err
	}
	for _, selector := range selectors {
		if !slices.ContainsFunc(
			libraries,
			func(lib types.Library) bool { return selector == lib.ID || selector == lib.Directory },
		) {
			return nil, werrors.NewBadRequestError("unknown library: " + selector)
		}
	}
	return slices.DeleteFunc(libraries, func(lib types.Library) bool { return !lib.FeedEnabled }), nil
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
	Version    int        `json:"v"`
	SpaceID    uint64     `json:"space"`
	Cutoff     time.Time  `json:"cutoff"`
	Since      *time.Time `json:"since"`
	Libraries  []string   `json:"libraries"`
	Targets    []string   `json:"targets"`
	LastFactAt time.Time  `json:"last_fact_at"`
	StoryID    string     `json:"id"`
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
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 2 || cursor.SpaceID != spaceID ||
		cursor.Cutoff.IsZero() ||
		cursor.LastFactAt.IsZero() ||
		cursor.LastFactAt.After(cursor.Cutoff) ||
		cursor.StoryID == "" ||
		len(cursor.StoryID) > 36 {
		return invalid()
	}
	if _, _, err := normalizeFollowFeed(types.FollowFeedRequest{TargetIDs: cursor.Targets}); err != nil {
		return invalid()
	}
	return &cursor, nil
}

// feedFacts shares the visibility policy with timelines and restricts evidence to enabled libraries.
// Manually corrected facts with no live sources retain their identity without attributing material to a library.
func (s *FollowService) feedFacts(ctx context.Context, spaceID uint64, cutoff time.Time, libraryIDs []string) *gorm.DB {
	sources := s.db.WithContext(ctx).Table("entry_facts ef").Select("1").
		Joins("JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id AND e.deleted_at IS NULL").
		Where("ef.space_id=f.space_id AND ef.fact_id=f.id AND ef.role='source'")
	return NewStoryService(s.db).visibleFacts(ctx, spaceID).Where("f.created_at<=?", cutoff).
		Where(`(EXISTS (?) OR (f.manual_corrected AND NOT EXISTS (?)))`,
			sources.Session(&gorm.Session{}).
				Where("e.library_id IN ? AND ef.created_at<=?", libraryIDs, cutoff),
			sources)
}

const followFactMatchSQL = `((follow.target_type='story' AND follow.target_id=f.story_id) OR
 (follow.target_type='entity' AND follow.target_id IN (f.subject_entity_id,f.object_entity_id)))`

func (s *FollowService) feedMatches(
	ctx context.Context,
	spaceID uint64,
	cutoff time.Time,
	targetIDs []string,
) *gorm.DB {
	q := s.db.WithContext(ctx).Table("follows follow").Select("1").
		Where("follow.space_id=? AND follow.created_at<=?", spaceID, cutoff).Where(followFactMatchSQL)
	if len(targetIDs) > 0 {
		q = q.Where("follow.target_id IN ?", targetIDs)
	}
	return q
}

// Feed reads event cards in a consistent transaction and never records a viewing watermark.
func (s *FollowService) Feed(ctx context.Context, request types.FollowFeedRequest) (*types.FollowFeedResponse, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	since, limit, err := normalizeFollowFeed(request)
	if err != nil {
		return nil, err
	}
	var cutoff time.Time
	if err = s.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&cutoff).Error; err != nil {
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
		if (len(request.Libraries) > 0 && !slices.Equal(request.Libraries, cursor.Libraries)) ||
			(request.Since != "" && (cursor.Since == nil || !since.Equal(*cursor.Since))) ||
			(len(request.TargetIDs) > 0 && !slices.Equal(request.TargetIDs, cursor.Targets)) {
			return nil, werrors.NewBadRequestError("cursor filters cannot change")
		}
		cutoff, since = cursor.Cutoff, cursor.Since
		request.Libraries, request.TargetIDs = cursor.Libraries, cursor.Targets
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

func (s *FollowService) feedSnapshot(
	ctx context.Context,
	spaceID uint64,
	request types.FollowFeedRequest,
	since *time.Time,
	limit int,
	cutoff time.Time,
	cursor *followCursor,
) (*types.FollowFeedResponse, error) {
	libraries, err := s.feedLibraries(ctx, spaceID, request.Libraries)
	if err != nil {
		return nil, err
	}
	libraryOptions := libraries
	if len(request.Libraries) > 0 {
		libraryOptions, err = s.feedLibraries(ctx, spaceID, nil)
		if err != nil {
			return nil, err
		}
	}
	response := &types.FollowFeedResponse{Results: []types.FollowFeedCard{}, Libraries: []types.FollowLibrary{}}
	ids := []string{}
	for _, lib := range libraries {
		ids = append(ids, lib.ID)
	}
	for _, lib := range libraryOptions {
		response.Libraries = append(
			response.Libraries,
			types.FollowLibrary{
				LibraryID:   lib.ID,
				LibraryName: lib.Name,
				DisplayName: lib.DisplayName(types.LanguageFromContextOrDefault(ctx)),
			},
		)
	}
	if len(ids) == 0 {
		return response, nil
	}
	facts := s.feedFacts(ctx, spaceID, cutoff, ids)
	matches := s.feedMatches(ctx, spaceID, cutoff, request.TargetIDs)
	times := facts.Session(&gorm.Session{}).
		Select("f.story_id,MIN(f.created_at) AS first_fact_at,MAX(f.created_at) AS last_fact_at").
		Group("f.story_id")
	matchedStories := facts.Session(&gorm.Session{}).Select("f.story_id").Where("EXISTS (?)", matches)
	query := s.db.WithContext(ctx).Table("stories st").
		Joins("JOIN (?) times ON times.story_id=st.id", times).
		Where("st.space_id=? AND st.merged_into_id='' AND st.id IN (?)", spaceID, matchedStories)
	if since != nil {
		query = query.Where("times.last_fact_at>?", *since)
	}
	if err := query.Count(&response.Total).Error; err != nil {
		return nil, err
	}
	if cursor != nil {
		query = query.Where(
			"times.last_fact_at<? OR (times.last_fact_at=? AND st.id>?)",
			cursor.LastFactAt,
			cursor.LastFactAt,
			cursor.StoryID,
		)
	}
	stories := []types.Story{}
	err = query.Select(`st.id,st.space_id,st.title,st.manual_corrected,st.merged_into_id,
 times.first_fact_at,times.last_fact_at`).
		Order("times.last_fact_at DESC,st.id").
		Limit(limit + 1).
		Scan(&stories).
		Error
	if err != nil {
		return nil, err
	}
	for _, story := range stories[:min(limit, len(stories))] {
		card, err := s.feedCard(ctx, story, cutoff, ids, request.TargetIDs)
		if err != nil {
			return nil, err
		}
		response.Results = append(response.Results, *card)
	}
	if len(stories) > limit {
		last := stories[limit-1]
		next := followCursor{
			Version:    2,
			SpaceID:    spaceID,
			Cutoff:     cutoff,
			Since:      since,
			Libraries:  request.Libraries,
			Targets:    request.TargetIDs,
			LastFactAt: last.LastFactAt,
			StoryID:    last.ID,
		}
		data, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		response.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return response, nil
}

func (s *FollowService) feedCard(
	ctx context.Context,
	story types.Story,
	cutoff time.Time,
	libraryIDs, targetIDs []string,
) (*types.FollowFeedCard, error) {
	query := s.feedFacts(ctx, story.SpaceID, cutoff, libraryIDs).Where("f.story_id=?", story.ID)
	card := &types.FollowFeedCard{
		Story:          story,
		RecentFacts:    []types.StoryFact{},
		MatchedFollows: []types.FollowTarget{},
	}
	recent := []types.Fact{}
	err := query.Session(&gorm.Session{}).Select("f.*").Order("f.created_at DESC,f.id").Limit(3).Scan(&recent).Error
	if err != nil {
		return nil, err
	}
	var trigger types.Fact
	matches := s.feedMatches(ctx, story.SpaceID, cutoff, targetIDs)
	if err := query.Session(&gorm.Session{}).Select("f.*").Where("EXISTS (?)", matches).
		Order("f.created_at DESC,f.id").Limit(1).Scan(&trigger).Error; err != nil {
		return nil, err
	}
	for _, fact := range append(recent, trigger) {
		item := types.StoryFact{Fact: fact, Sources: []types.FactSource{}}
		err := s.db.WithContext(ctx).Table("entry_facts ef").
			Select(`ef.entry_id,e.library_id,source_library.name AS library_name,e.title,ef.role,ef.quote,ef.manual,
 COALESCE(e.metadata#>>'{intake,source_info,original_url}',e.metadata->>'original_url','') AS original_url`).
			Joins("JOIN entries e ON e.id=ef.entry_id AND e.space_id=ef.space_id AND e.deleted_at IS NULL").
			Joins(`JOIN libraries source_library ON source_library.id=e.library_id
 AND source_library.space_id=e.space_id`).
			Where(`ef.space_id=? AND ef.fact_id=? AND ef.role='source'
 AND ef.created_at<=? AND e.library_id IN ?`, story.SpaceID, fact.ID, cutoff, libraryIDs).
			Order("ef.created_at,ef.entry_id").Scan(&item.Sources).Error
		if err != nil {
			return nil, err
		}
		item.NoSource = len(item.Sources) == 0
		if len(card.RecentFacts) < len(recent) {
			card.RecentFacts = append(card.RecentFacts, item)
		} else {
			card.TriggerFact = item
		}
	}
	// Match labels use the same eligible facts as card selection, in one bounded query per card.
	targets := s.followTargets(ctx, story.SpaceID).Where("follow.created_at<=?", cutoff).
		Where("EXISTS (?)", query.Session(&gorm.Session{}).Select("1").Where(followFactMatchSQL))
	if len(targetIDs) > 0 {
		targets = targets.Where("follow.target_id IN ?", targetIDs)
	}
	if err := targets.Scan(&card.MatchedFollows).Error; err != nil {
		return nil, err
	}
	return card, nil
}
