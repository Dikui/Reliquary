package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
)

func TestFollowHTTPPostgresActionsFeedAndViewedSnapshot(t *testing.T) {
	f := newPersonalAuthFixture(t)
	follows := service.NewFollowService(f.db)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(follows))
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/follows", "", "invalid-key", true)
	require.Equal(t, http.StatusUnauthorized, r.Code)
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/library", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code)
	var library struct{ Data types.Library }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &library))
	entity := types.Entity{LibraryID: library.Data.ID, ID: uuid.NewString(), SpaceID: types.SingleSpaceID, CanonicalName: "HTTP followed entity", NormalizedName: "http followed entity", EntityType: "company"}
	topic := types.Topic{LibraryID: library.Data.ID, ID: uuid.NewString(), SpaceID: types.SingleSpaceID, Name: "HTTP followed topic"}
	require.NoError(t, f.db.Create(&entity).Error)
	require.NoError(t, f.db.Create(&topic).Error)
	for _, target := range [][2]string{{"entity", entity.ID}, {"topic", topic.ID}} {
		for range 2 {
			r = personalRequest(t, f.router, http.MethodPut, "/api/v1/follows/"+target[0]+"/"+target[1], "", f.key, true)
			require.Equal(t, http.StatusOK, r.Code, r.Body.String())
		}
	}
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/follows", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code)
	var targets struct{ Data []types.FollowTarget }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &targets))
	require.Len(t, targets.Data, 2)
	article := types.Entry{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, LibraryID: library.Data.ID, Type: "manual", Title: "HTTP feed article", SourceKind: "article", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now().UTC().Add(-time.Hour)}
	require.NoError(t, f.db.Create(&article).Error)
	require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: types.SingleSpaceID, EntryID: article.ID, EntityID: entity.ID}).Error)
	require.NoError(t, f.db.Create(&types.EntryTopic{SpaceID: types.SingleSpaceID, EntryID: article.ID, TopicID: topic.ID}).Error)
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/follows/state", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code)
	var state struct{ Data types.FollowFeedState }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &state))
	require.EqualValues(t, 1, state.Data.NewCount)
	require.Nil(t, state.Data.LastViewedAt)
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/feed", fmt.Sprintf(`{"include_seen":true,"target_ids":[%q],"collapse_similar":false}`, topic.ID), f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var feed struct{ Data types.FollowFeedResponse }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &feed))
	require.Len(t, feed.Data.Results, 1)
	require.Equal(t, article.ID, feed.Data.Results[0].EntryID)
	require.Len(t, feed.Data.Results[0].MatchedFollows, 2)
	require.True(t, feed.Data.Results[0].IsNew)
	require.NotEmpty(t, feed.Data.Results[0].Snippets)
	require.Nil(t, feed.Data.LastViewedAt)
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/follows/state", "", f.key, true)
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &state))
	require.Nil(t, state.Data.LastViewedAt, "HTTP feed is also read-only until the page acknowledges it")
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/view", fmt.Sprintf(`{"viewed_at":%q}`, feed.Data.ViewedAt.Format(time.RFC3339Nano)), f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &state))
	require.Zero(t, state.Data.NewCount)
	require.Equal(t, feed.Data.ViewedAt.UnixMicro(), state.Data.LastViewedAt.UnixMicro())
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/feed", `{}`, f.key, true)
	require.Equal(t, http.StatusOK, r.Code)
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &feed))
	require.Empty(t, feed.Data.Results)
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/view", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code)
	for _, body := range []string{`{"limit":101}`, `{"since":"invalid"}`, `{"target_ids":[""]}`, `not-json`} {
		r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/feed", body, f.key, true)
		require.Equal(t, http.StatusBadRequest, r.Code, r.Body.String())
	}
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/view", `{"viewed_at":"invalid"}`, f.key, true)
	require.Equal(t, http.StatusBadRequest, r.Code)
	r = personalRequest(t, f.router, http.MethodPut, "/api/v1/follows/article/"+article.ID, "", f.key, true)
	require.Equal(t, http.StatusBadRequest, r.Code)
	for _, target := range [][2]string{{"entity", entity.ID}, {"topic", topic.ID}} {
		for range 2 {
			r = personalRequest(t, f.router, http.MethodDelete, "/api/v1/follows/"+target[0]+"/"+target[1], "", f.key, true)
			require.Equal(t, http.StatusOK, r.Code, r.Body.String())
		}
	}
	var logs []types.OperationLog
	require.NoError(t, f.db.Where("operation_type IN ('follow','unfollow')").Find(&logs).Error)
	require.Len(t, logs, 4)
	for _, log := range logs {
		require.Contains(t, log.Actor, "api_key:")
	}
}
