package repository

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"testing"
)

const taskDeadLettersTestDDL = `
CREATE TABLE IF NOT EXISTS task_dead_letters (
    id          BIGSERIAL PRIMARY KEY,
    space_id   INTEGER NOT NULL,
    task_type   VARCHAR(64) NOT NULL,
    scope       VARCHAR(32) NOT NULL,
    scope_id    VARCHAR(64) NOT NULL,
    related_id  VARCHAR(64) NOT NULL DEFAULT '',
    payload     JSONB NOT NULL,
    last_error  TEXT NOT NULL DEFAULT '',
    fail_count  INTEGER NOT NULL,
    failed_at   TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
`

func setupTaskDeadLetterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(taskDeadLettersTestDDL).Error)
	return db
}

func makeDeadLetter(taskType, scope, scopeID, relatedID, lastErr string) *types.TaskDeadLetter {
	return &types.TaskDeadLetter{
		SpaceID:   1,
		TaskType:  taskType,
		Scope:     scope,
		ScopeID:   scopeID,
		RelatedID: relatedID,
		Payload:   json.RawMessage(`{"x":1}`),
		LastError: lastErr,
		FailCount: 5,
	}
}

// TestTaskDeadLetter_Insert_DefaultsAndAssignsID covers the empty-payload
// fallback and ID assignment.
func TestTaskDeadLetter_Insert_DefaultsAndAssignsID(t *testing.T) {
	db := setupTaskDeadLetterTestDB(t)
	repo := NewTaskDeadLetterRepository(db)
	ctx := context.Background()

	dl := &types.TaskDeadLetter{
		SpaceID:   1,
		TaskType:  "document:process",
		ScopeID:   "library",
		FailCount: 3,
		// Scope intentionally empty — should default to "unknown".
		// Payload intentionally nil — should default to "{}".
	}
	require.NoError(t, repo.Insert(ctx, dl))
	assert.NotZero(t, dl.ID)
	assert.Equal(t, types.TaskScopeUnknown, dl.Scope)
	assert.Equal(t, json.RawMessage("{}"), dl.Payload)
}

// TestTaskDeadLetter_Insert_RejectsMissingFields verifies the guard
// against rows that would leave the table without the columns ops queries
// rely on.
func TestTaskDeadLetter_Insert_RejectsMissingFields(t *testing.T) {
	db := setupTaskDeadLetterTestDB(t)
	repo := NewTaskDeadLetterRepository(db)
	ctx := context.Background()

	assert.Error(t, repo.Insert(ctx, nil))
	assert.Error(t, repo.Insert(ctx, &types.TaskDeadLetter{ScopeID: "library"}))

	var n int64
	db.Table("task_dead_letters").Count(&n)
	assert.Equal(t, int64(0), n)
}

// TestTaskDeadLetter_ListByScope_NewestFirstAndCursored exercises the
// cursor pagination path used by the ops console.
func TestTaskDeadLetter_ListByScope_NewestFirstAndCursored(t *testing.T) {
	db := setupTaskDeadLetterTestDB(t)
	repo := NewTaskDeadLetterRepository(db)
	ctx := context.Background()

	// Insert 5 rows for library-A and 2 for library-B.
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Insert(ctx, makeDeadLetter("document:process", "library", "library-A", "k", "boom")))
	}
	require.NoError(t, repo.Insert(ctx, makeDeadLetter("document:process", "library", "library-B", "k", "boom")))
	require.NoError(t, repo.Insert(ctx, makeDeadLetter("document:process", "library", "library-B", "k", "boom")))

	// First page of 2 from library-A, newest first.
	page1, cursor, err := repo.ListByScope(ctx, "library", "library-A", "", 2)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.True(t, page1[0].ID > page1[1].ID, "newest first")
	require.NotEmpty(t, cursor)

	// Second page of 2.
	page2, cursor, err := repo.ListByScope(ctx, "library", "library-A", cursor, 2)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.True(t, page1[1].ID > page2[0].ID, "page2 should continue past page1")
	require.NotEmpty(t, cursor)

	// Last page — only 1 row left, cursor goes empty since len < limit.
	page3, cursor, err := repo.ListByScope(ctx, "library", "library-A", cursor, 2)
	require.NoError(t, err)
	require.Len(t, page3, 1)
	assert.Empty(t, cursor)

	// library-B is isolated.
	pageB, _, err := repo.ListByScope(ctx, "library", "library-B", "", 10)
	require.NoError(t, err)
	require.Len(t, pageB, 2)
}

// TestTaskDeadLetter_ListByScope_RejectsMissingScope guards the input
// validation in the public method.
func TestTaskDeadLetter_ListByScope_RejectsMissingScope(t *testing.T) {
	db := setupTaskDeadLetterTestDB(t)
	repo := NewTaskDeadLetterRepository(db)
	ctx := context.Background()

	_, _, err := repo.ListByScope(ctx, "", "library", "", 10)
	assert.Error(t, err)
	_, _, err = repo.ListByScope(ctx, "library", "", "", 10)
	assert.Error(t, err)
}

// TestTaskDeadLetter_ListByTaskType_FiltersAndPaginates is the cross-Library
// view: "all document:process failures" regardless of which Library they
// belong to.
func TestTaskDeadLetter_ListByTaskType_FiltersAndPaginates(t *testing.T) {
	db := setupTaskDeadLetterTestDB(t)
	repo := NewTaskDeadLetterRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Insert(ctx, makeDeadLetter("document:process", "library", "library-A", "k1", "")))
	require.NoError(t, repo.Insert(ctx, makeDeadLetter("summary:gen", "library", "library-A", "k2", "")))
	require.NoError(t, repo.Insert(ctx, makeDeadLetter("summary:gen", "library", "library-B", "k3", "")))
	require.NoError(t, repo.Insert(ctx, makeDeadLetter("document:process", "library", "library-B", "k4", "")))

	rows, _, err := repo.ListByTaskType(ctx, "summary:gen", "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, "summary:gen", r.TaskType)
	}

	_, _, err = repo.ListByTaskType(ctx, "", "", 10)
	assert.Error(t, err)
}

// TestTaskDeadLetter_DeleteByID_IsIdempotent confirms a missing row does
// not produce an error — operators triggering concurrent deletes should
// see clean success.
func TestTaskDeadLetter_DeleteByID_IsIdempotent(t *testing.T) {
	db := setupTaskDeadLetterTestDB(t)
	repo := NewTaskDeadLetterRepository(db)
	ctx := context.Background()

	dl := makeDeadLetter("document:process", "library", "library", "k", "")
	require.NoError(t, repo.Insert(ctx, dl))

	require.NoError(t, repo.DeleteByID(ctx, dl.ID))
	// Second delete on the same id should silently succeed.
	require.NoError(t, repo.DeleteByID(ctx, dl.ID))
	// Delete of unknown id should silently succeed.
	require.NoError(t, repo.DeleteByID(ctx, 99999))

	rows, _, err := repo.ListByScope(ctx, "library", "library", "", 10)
	require.NoError(t, err)
	assert.Len(t, rows, 0)
}
