package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reliquary/internal/types"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type followFixture struct {
	*entrySearchFixture
	follows *FollowService
}

func newFollowFixture(t *testing.T) *followFixture {
	t.Helper()
	h := newEntrySearchFixture(t)
	h.ctx = types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalWebUser, ID: "follow-owner"})
	entities := []types.Entity{
		entityRecord("entity-follow", "Followed company", "company"),
		entityRecord("entity-other", "Other company", "company"),
		entityRecord("entity-second", "Second followed company", "company"),
	}
	require.NoError(t, h.db.Create(&entities).Error)
	return &followFixture{entrySearchFixture: h, follows: NewFollowService(h.db)}
}

func (h *followFixture) link(t *testing.T, articleID string, entity, secondary bool) {
	t.Helper()
	var when time.Time
	require.NoError(t, h.db.Table("entries").Select("created_at").Where("id=?", articleID).Scan(&when).Error)
	if entity {
		require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: articleID, EntityID: "entity-follow", CreatedAt: when}).Error)
	}
	if secondary {
		require.NoError(
			t,
			h.db.Create(
				&types.EntryEntity{SpaceID: 1, EntryID: articleID, EntityID: "entity-second", CreatedAt: when},
			).Error,
		)
	}
}

func (h *followFixture) followBoth(t *testing.T) {
	t.Helper()
	_, err := h.follows.Follow(h.ctx, "entity", "entity-follow")
	require.NoError(t, err)
	_, err = h.follows.Follow(h.ctx, "entity", "entity-second")
	require.NoError(t, err)
}

func TestFollowsPostgresActionsIdempotencyAndLogs(t *testing.T) {
	h := newFollowFixture(t)
	for _, id := range []string{"entity-follow", "entity-second"} {
		targetType := "entity"
		first, err := h.follows.Follow(h.ctx, targetType, id)
		require.NoError(t, err)
		second, err := h.follows.Follow(h.ctx, targetType, id)
		require.NoError(t, err)
		require.Equal(t, first.CreatedAt.UnixMicro(), second.CreatedAt.UnixMicro())
		require.Equal(t, id, first.TargetID)
		require.NotEmpty(t, first.Name)
	}
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	var logs []types.OperationLog
	require.NoError(t, h.db.Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, "follow", log.OperationType)
		require.Equal(t, "web_user:follow-owner", log.Actor)
		require.JSONEq(t, "null", string(log.Before))
		var after types.FollowTarget
		require.NoError(t, json.Unmarshal(log.After, &after))
		require.Equal(t, log.ObjectID, after.TargetID)
	}
	apiCtx := types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "follow-key"})
	for _, id := range []string{"entity-follow", "entity-second"} {
		targetType := "entity"
		require.NoError(t, h.follows.Unfollow(apiCtx, targetType, id))
		require.NoError(t, h.follows.Unfollow(apiCtx, targetType, id))
	}
	items, err = h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Empty(t, items)
	logs = nil
	require.NoError(t, h.db.Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 4)
	for _, log := range logs[2:] {
		require.Equal(t, "unfollow", log.OperationType)
		require.Equal(t, "api_key:follow-key", log.Actor)
		require.JSONEq(t, "null", string(log.After))
	}
	otherCtx := context.WithValue(h.ctx, types.SpaceIDContextKey, uint64(2))
	_, err = h.follows.Follow(otherCtx, "entity", "entity-follow")
	require.Error(t, err)
	for _, pair := range [][2]string{{"article", "entity-follow"}, {"entity", ""}, {"topic", "missing"}} {
		_, err := h.follows.Follow(h.ctx, pair[0], pair[1])
		require.Error(t, err)
	}
	_, err = h.follows.Follow(context.Background(), "entity", "entity-follow")
	require.Error(t, err)
}

func TestFollowsPostgresLogFailureRollsBackChanges(t *testing.T) {
	h := newFollowFixture(t)
	_, err := h.follows.Follow(h.ctx, "entity", "entity-follow")
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_follow_log() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'fixture log rejected'; END $$;
        CREATE TRIGGER reject_follow_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_follow_log()`).Error)
	_, err = h.follows.Follow(h.ctx, "entity", "entity-second")
	require.Error(t, err)
	require.Error(t, h.follows.Unfollow(h.ctx, "entity", "entity-follow"))
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "entity-follow", items[0].TargetID)
}

func TestFollowsPostgresConcurrentActionsRemainIdempotent(t *testing.T) {
	h := newFollowFixture(t)
	run := func(action func() error) {
		var wg sync.WaitGroup
		failures := make(chan error, 8)
		start := make(chan struct{})
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				failures <- action()
			}()
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
	}
	for _, pair := range [][2]string{{"entity", "entity-follow"}, {"entity", "entity-second"}} {
		run(func() error { _, err := h.follows.Follow(h.ctx, pair[0], pair[1]); return err })
		run(func() error { return h.follows.Unfollow(h.ctx, pair[0], pair[1]) })
	}
	var count int64
	require.NoError(t, h.db.Model(&types.OperationLog{}).Count(&count).Error)
	require.EqualValues(t, 4, count)
	require.NoError(t, h.db.Model(&types.Follow{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestFollowsPostgresMergeTransferDeduplicationAndDeletion(t *testing.T) {
	for _, followKeep := range []bool{false, true} {
		t.Run(fmt.Sprint(followKeep), func(t *testing.T) {
			h := newFollowFixture(t)
			_, err := h.follows.Follow(h.ctx, "entity", "entity-other")
			require.NoError(t, err)
			earliest := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Microsecond)
			require.NoError(t, h.db.Model(&types.Follow{}).Where("target_id='entity-other'").Update("created_at", earliest).Error)
			if followKeep {
				_, err = h.follows.Follow(h.ctx, "entity", "entity-follow")
				require.NoError(t, err)
			}
			article := h.add(t, "merged-article", "article", "2026-10-01", "event", "semanticlaunch", 0)
			require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: article.ID, EntityID: "entity-other"}).Error)
			require.NoError(t, h.db.Model(&types.Library{}).Where("id='search-library'").Update("summary_model_id", "entity-chat").Error)
			entities := NewEntityService(h.db, nil)
			require.NoError(t, entities.MergeEntities(h.ctx, "entity-follow", "entity-other"))
			items, err := h.follows.ListFollows(h.ctx)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, "entity-follow", items[0].TargetID)
			require.Equal(t, earliest.UnixMicro(), items[0].CreatedAt.UnixMicro())
			require.NoError(t, entities.MergeEntities(h.ctx, "entity-follow", "entity-other"))
			redirected, err := h.follows.Follow(h.ctx, "entity", "entity-other")
			require.NoError(t, err)
			require.Equal(t, "entity-follow", redirected.TargetID)
			require.NoError(t, h.follows.Unfollow(h.ctx, "entity", "entity-other"))
			items, err = h.follows.ListFollows(h.ctx)
			require.NoError(t, err)
			require.Empty(t, items)
		})
	}
	h := newFollowFixture(t)
	h.followBoth(t)
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "entity", items[0].TargetType)
	// Foreign keys also clean direct object deletion and forbid dangling targets.
	require.NoError(t, h.db.Where("id IN ?", []string{"entity-follow", "entity-second"}).Delete(&types.Entity{}).Error)
	var count int64
	require.NoError(t, h.db.Model(&types.Follow{}).Count(&count).Error)
	require.Zero(t, count)
	require.Error(t, h.db.Create(&types.Follow{SpaceID: 1, TargetType: "entity", TargetID: "missing"}).Error)
	require.NoError(t, h.follows.Unfollow(h.ctx, "entity", "entity-second"))
}

func TestFollowsPostgresMergeLogFailureRollsBackTransfer(t *testing.T) {
	h := newFollowFixture(t)
	_, err := h.follows.Follow(h.ctx, "entity", "entity-other")
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_merge_follow_log() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN IF NEW.operation_type='merge_entities' THEN RAISE EXCEPTION 'fixture merge log rejected'; END IF; RETURN NEW; END $$;
        CREATE TRIGGER reject_merge_follow_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_merge_follow_log()`).Error)
	require.Error(t, NewEntityService(h.db, nil).MergeEntities(h.ctx, "entity-follow", "entity-other"))
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "entity-other", items[0].TargetID)
	var merged types.Entity
	require.NoError(t, h.db.Where("id='entity-other'").First(&merged).Error)
	require.Empty(t, merged.MergedIntoID)
}
