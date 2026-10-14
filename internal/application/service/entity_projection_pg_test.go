package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/config"
	"reliquary/internal/graphreplica"
	"reliquary/internal/types"
)

func TestIsolatedEntityProjectionFollowsCreateRenameAndMerge(t *testing.T) {
	if os.Getenv("RELIQUARY_TEST_NEO4J_URI") == "" {
		t.Skip("set RELIQUARY_TEST_NEO4J_URI to verify the real entity projection")
	}
	db, ctx, svc := entityFixture(t)
	store, err := graphreplica.NewDrillStore(config.Neo4jConfig{URI: os.Getenv("RELIQUARY_TEST_NEO4J_URI"), Username: os.Getenv("RELIQUARY_TEST_NEO4J_USERNAME"), Password: os.Getenv("RELIQUARY_TEST_NEO4J_PASSWORD")}, "reliquary_drill_"+strings.ReplaceAll(uuid.NewString(), "-", "")[:16])
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.ClearDrill(context.Background())); require.NoError(t, store.Close()) })
	replica := graphreplica.NewService(db, store, nil)
	sync := func() {
		t.Helper()
		var ids []string
		require.NoError(t, db.Model(&types.GraphPendingSync{}).Where("completed_at IS NULL").Distinct().Pluck("entity_id", &ids).Error)
		require.NotEmpty(t, ids, "isolated entity mutations must produce durable notifications")
		for _, id := range ids {
			require.NoError(t, replica.Sync(ctx, 1, id))
		}
		result, err := replica.Verify(ctx)
		require.NoError(t, err)
		require.Empty(t, result.Differences)
		require.Equal(t, result.PGHash, result.Neo4jHash)
	}
	keep := entityRecord("projection-keep", "Alice", "person")
	other := entityRecord("projection-merge", "Alice Alias", "person")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, []types.Entity{keep, other}); return err }))
	sync()
	// No relation references either entity, including as a graph endpoint.
	_, err = svc.RenameEntity(ctx, keep.ID, "Alice Renamed")
	require.NoError(t, err)
	sync()
	require.NoError(t, svc.MergeEntities(ctx, keep.ID, other.ID))
	sync()
}

func TestEntityProjectionNotificationFailureRollsBackIdentity(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	keep := entityRecord("projection-keep", "Alice", "person")
	other := entityRecord("projection-merge", "Alice Alias", "person")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, []types.Entity{keep, other}); return err }))
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_projection_notification() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'projection unavailable'; END $$;
 CREATE TRIGGER reject_projection_notification BEFORE INSERT ON graph_pending_sync FOR EACH ROW EXECUTE FUNCTION reject_projection_notification()`).Error)
	created := entityRecord("projection-new", "New Speaker", "person")
	err := db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, []types.Entity{created}); return err })
	require.ErrorContains(t, err, "projection unavailable")
	var count int64
	require.NoError(t, db.Model(&types.Entity{}).Where("id=?", created.ID).Count(&count).Error)
	require.Zero(t, count)
	_, err = svc.RenameEntity(ctx, keep.ID, "Changed Speaker")
	require.ErrorContains(t, err, "projection unavailable")
	require.Equal(t, "Alice", readNameEntity(t, db, keep.ID).CanonicalName)
	err = svc.MergeEntities(ctx, keep.ID, other.ID)
	require.ErrorContains(t, err, "projection unavailable")
	require.Empty(t, readNameEntity(t, db, other.ID).MergedIntoID)
	// Failed outer transactions must not leak notifications either.
	require.NoError(t, db.Exec("DROP TRIGGER reject_projection_notification ON graph_pending_sync").Error)
	err = db.Transaction(func(tx *gorm.DB) error {
		if _, err := persistEntities(tx, []types.Entity{created}); err != nil {
			return err
		}
		return errors.New("abort")
	})
	require.ErrorContains(t, err, "abort")
	require.NoError(t, db.Model(&types.GraphPendingSync{}).Where("entity_id=?", created.ID).Count(&count).Error)
	require.Zero(t, count)
}
