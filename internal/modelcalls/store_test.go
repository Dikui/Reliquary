package modelcalls_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/maintenance"
	"reliquary/internal/modelcalls"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func tokens(n int64) *int64 { return &n }
func TestSummaryTrendAndRetention(t *testing.T) {
	db := pgtest.Open(t)
	store := modelcalls.NewStore(db)
	ctx := context.Background()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	rows := []types.ModelCall{
		{ModelID: "a", Purpose: types.CallPurposeIntakeReview, InputTokens: tokens(100), OutputTokens: tokens(20), Cost: price(1), CostStatus: "calculated", Success: true, DurationMS: 100},
		{ModelID: "a", Purpose: types.CallPurposeIntakeReview, CostStatus: "unknown", Success: false, DurationMS: 300},
		{ModelID: "b", Purpose: types.CallPurposeRerank, InputTokens: tokens(50), Cost: price(2), CostStatus: "calculated", Success: true, DurationMS: 200},
		{ModelID: "a", Purpose: types.CallPurposeIntakeReview, InputTokens: tokens(200), OutputTokens: tokens(30), Cost: price(3), CostStatus: "calculated", Success: true, DurationMS: 400},
	}
	for i := range rows {
		rows[i].ID = uuid.NewString()
		rows[i].SpaceID = 1
		rows[i].Currency = "USD"
		rows[i].RequestedAt = start
		rows[i].Attempt = 1
		if i == 3 {
			rows[i].RequestedAt = start.AddDate(0, 0, 1)
		}
		require.NoError(t, store.Write(ctx, &rows[i]))
	}
	// Same period, different currency and space cannot contaminate USD totals.
	other := rows[0]
	other.ID = uuid.NewString()
	other.Currency = "CNY"
	require.NoError(t, store.Write(ctx, &other))
	other.ID = uuid.NewString()
	other.SpaceID = 2
	require.NoError(t, store.Write(ctx, &other))
	f := modelcalls.Filter{SpaceID: 1, From: &start}
	to := start.AddDate(0, 0, 2)
	f.To = &to
	trend, err := store.Summary(ctx, f, "day", nil)
	require.NoError(t, err)
	require.Len(t, trend, 3)
	var usd []modelcalls.Summary
	for _, s := range trend {
		if s.Currency == "USD" {
			usd = append(usd, s)
		}
	}
	sort.Slice(usd, func(i, j int) bool { return usd[i].Period.Before(usd[j].Period) })
	require.EqualValues(t, 3, usd[0].Calls)
	require.EqualValues(t, 150, *usd[0].InputTokens)
	require.EqualValues(t, 20, *usd[0].OutputTokens)
	require.InDelta(t, 3, *usd[0].Cost, 1e-10)
	require.EqualValues(t, 1, usd[0].UnknownInputCalls)
	require.EqualValues(t, 2, usd[0].UnknownOutputCalls)
	require.InDelta(t, 1.0/3, usd[0].FailureRate, 1e-10)
	require.InDelta(t, 200, usd[0].AverageDurationMS, 1e-10)
	require.EqualValues(t, 1, usd[1].Calls)
	require.EqualValues(t, 200, *usd[1].InputTokens)
	require.InDelta(t, 3, *usd[1].Cost, 1e-10)
	for _, period := range []string{"week", "month"} {
		summary, e := store.Summary(ctx, f, period, []string{"model", "purpose"})
		require.NoError(t, e)
		require.Len(t, summary, 3)
		for _, s := range summary {
			if s.ModelID == "a" && s.Currency == "USD" {
				require.EqualValues(t, 3, s.Calls)
				require.EqualValues(t, 300, *s.InputTokens)
				require.EqualValues(t, 50, *s.OutputTokens)
				require.InDelta(t, 4, *s.Cost, 1e-10)
				require.InDelta(t, 800.0/3, s.AverageDurationMS, 1e-10)
			}
		}
	}
	f.ModelID = "a"
	f.EntryID = "missing"
	list, total, err := store.List(ctx, f, 10, 0)
	require.NoError(t, err)
	require.Empty(t, list)
	require.Zero(t, total)
	t.Setenv("RELIQUARY_MODEL_CALL_RETENTION_DAYS", "0")
	require.NoError(t, store.Prune(ctx))
	var count int64
	require.NoError(t, db.Model(&types.ModelCall{}).Count(&count).Error)
	require.EqualValues(t, 6, count)
	t.Setenv("RELIQUARY_MODEL_CALL_RETENTION_DAYS", "1")
	require.NoError(t, store.Prune(ctx))
	require.NoError(t, db.Model(&types.ModelCall{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMigration22PreservesPreviousRowsAndReplicaOwnership(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("DROP TABLE model_calls").Error)
	require.NoError(t, db.Exec("UPDATE schema_migrations SET version=17").Error)
	require.NoError(t, db.Exec(`INSERT INTO models(id,space_id,name,type,source,parameters) VALUES('price-before',1,'before',?,'remote','{"pricing":{"input_per_million":2,"output_per_million":4,"currency":"USD"}}')`, types.ModelTypeGeneration).Error)
	var before, after []string
	require.NoError(t, db.Raw(`SELECT to_jsonb(t)::text FROM models t ORDER BY id`).Scan(&before).Error)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "versioned", "000022_model_calls.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	require.NoError(t, db.Exec("UPDATE schema_migrations SET version=22").Error)
	require.NoError(t, db.Raw(`SELECT to_jsonb(t)::text FROM models t ORDER BY id`).Scan(&after).Error)
	require.Equal(t, before, after)
	require.NoError(t, maintenance.ValidateClassification(ctx, db))
	_, ok := maintenance.BackupOnlyTables["model_calls"]
	require.True(t, ok)
	_, ok = maintenance.PersistentTables["model_calls"]
	require.False(t, ok)
}
