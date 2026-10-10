package container

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

const resetPendingKnowledgeDDL = `
CREATE TABLE IF NOT EXISTS knowledges (
    id              VARCHAR(64) PRIMARY KEY,
    parse_status    VARCHAR(32) NOT NULL DEFAULT 'pending',
    summary_status  VARCHAR(32) NOT NULL DEFAULT 'none',
    pending_subtasks_count INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT,
    updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at      TIMESTAMPTZ
);
`

const resetPendingSyncLogDDL = `
CREATE TABLE IF NOT EXISTS sync_logs (
    id              VARCHAR(64) PRIMARY KEY,
    data_source_id  VARCHAR(64) NOT NULL DEFAULT '',
    tenant_id       INTEGER NOT NULL DEFAULT 0,
    status          VARCHAR(32) NOT NULL,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    error_message   TEXT,
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
`

const resetPendingSpansDDL = `
CREATE TABLE IF NOT EXISTS knowledge_processing_spans (
    id              BIGSERIAL PRIMARY KEY,
    knowledge_id    VARCHAR(64) NOT NULL,
    attempt         INTEGER NOT NULL DEFAULT 1,
    span_id         VARCHAR(64) NOT NULL,
    parent_span_id  VARCHAR(64),
    name            VARCHAR(255) NOT NULL,
    kind            VARCHAR(16) NOT NULL,
    status          VARCHAR(16) NOT NULL,
    error_code      VARCHAR(64),
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    duration_ms     INTEGER,
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (knowledge_id, attempt, span_id)
);
`

const resetPendingOpsDDL = `
CREATE TABLE IF NOT EXISTS task_pending_ops (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 0,
    task_type   VARCHAR(64) NOT NULL,
    scope       VARCHAR(32) NOT NULL,
    scope_id    VARCHAR(64) NOT NULL,
    op          VARCHAR(32) NOT NULL,
    dedup_key   VARCHAR(128) NOT NULL DEFAULT '',
    payload     TEXT NOT NULL DEFAULT '{}',
    fail_count  INTEGER NOT NULL DEFAULT 0,
    enqueued_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    claimed_at  TIMESTAMPTZ
);
`

const resetPendingKnowledgeBasesDDL = `
CREATE TABLE IF NOT EXISTS knowledge_bases (
    id          VARCHAR(64) PRIMARY KEY,
    tenant_id   INTEGER NOT NULL DEFAULT 0,
    deleted_at  TIMESTAMPTZ
);
`

func setupResetPendingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Error)
	require.NoError(t, db.Exec(resetPendingKnowledgeDDL).Error)
	require.NoError(t, db.Exec(resetPendingSyncLogDDL).Error)
	require.NoError(t, db.Exec(resetPendingSpansDDL).Error)
	require.NoError(t, db.Exec(resetPendingOpsDDL).Error)
	require.NoError(t, db.Exec(resetPendingKnowledgeBasesDDL).Error)
	return db
}

func TestResetPendingTasks_KnowledgeFreshInDistributedMode(t *testing.T) {
	db := setupResetPendingDB(t)
	fresh := time.Now().Add(-5 * time.Minute)
	require.NoError(t, db.Exec(
		`INSERT INTO knowledges (id, parse_status, updated_at) VALUES (?, ?, ?)`,
		"k-fresh", types.ParseStatusProcessing, fresh,
	).Error)

	t.Setenv("REDIS_ADDR", "redis:6379")
	resetPendingTasks(db)

	var status string
	require.NoError(t, db.Raw(
		`SELECT parse_status FROM knowledges WHERE id = ?`, "k-fresh",
	).Row().Scan(&status))
	assert.Equal(t, types.ParseStatusProcessing, status)
}

func TestResetPendingTasks_DistributedModePreservesEveryStage(t *testing.T) {
	cases := []struct {
		name        string
		parseStatus string
	}{
		{"pending", types.ParseStatusPending},
		{"docreader_chunking_embedding", types.ParseStatusProcessing},
		{"multimodal", types.ParseStatusProcessing},
		{"postprocess", types.ParseStatusFinalizing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupResetPendingDB(t)
			stale := time.Now().Add(-2 * time.Hour)
			require.NoError(t, db.Exec(
				`INSERT INTO knowledges (id, parse_status, updated_at) VALUES (?, ?, ?)`,
				"k-active-span", tc.parseStatus, stale,
			).Error)

			t.Setenv("REDIS_ADDR", "redis:6379")
			resetPendingTasks(db)

			var status string
			require.NoError(t, db.Raw(
				`SELECT parse_status FROM knowledges WHERE id = ?`, "k-active-span",
			).Row().Scan(&status))
			assert.Equal(t, tc.parseStatus, status,
				"distributed tasks belong to Asynq/housekeeping, not startup reset")
		})
	}
}

func TestResetPendingTasks_DistributedSummaryTaskSurvivesRestart(t *testing.T) {
	db := setupResetPendingDB(t)
	stale := time.Now().Add(-2 * time.Hour)
	require.NoError(t, db.Exec(
		`INSERT INTO knowledges (id, parse_status, summary_status, updated_at)
		 VALUES (?, ?, ?, ?)`,
		"k-summary", types.ParseStatusCompleted, types.SummaryStatusProcessing, stale,
	).Error)

	t.Setenv("REDIS_ADDR", "redis:6379")
	resetPendingTasks(db)

	var status string
	require.NoError(t, db.Raw(
		`SELECT summary_status FROM knowledges WHERE id = ?`, "k-summary",
	).Row().Scan(&status))
	assert.Equal(t, types.SummaryStatusProcessing, status)
}

func TestResetPendingTasks_SyncLogStaleRunning(t *testing.T) {
	db := setupResetPendingDB(t)
	stale := time.Now().Add(-2 * time.Hour)
	require.NoError(t, db.Exec(
		`INSERT INTO sync_logs (id, status, started_at) VALUES (?, ?, ?)`,
		"sync-1", types.SyncLogStatusRunning, stale,
	).Error)

	t.Setenv("REDIS_ADDR", "redis:6379")
	resetPendingTasks(db)

	var status string
	var finishedAt *time.Time
	require.NoError(t, db.Raw(
		`SELECT status, finished_at FROM sync_logs WHERE id = ?`, "sync-1",
	).Row().Scan(&status, &finishedAt))
	assert.Equal(t, types.SyncLogStatusFailed, status)
	require.NotNil(t, finishedAt)
}
