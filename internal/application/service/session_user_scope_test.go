package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func testSessionScopeContext(tenantID uint64, userID string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	if userID != "" {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	return ctx
}

func newTestSessionService(t *testing.T) (*sessionService, *gorm.DB) {
	t.Helper()

	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Session{}))

	return &sessionService{
		sessionRepo: repository.NewSessionRepository(db),
	}, db
}

func TestGetSessionIsScopedToCurrentUser(t *testing.T) {
	svc, db := newTestSessionService(t)
	aliceSession := &types.Session{
		TenantID: 1,
		UserID:   "alice",
		Title:    "alice private session",
	}
	require.NoError(t, db.Create(aliceSession).Error)
	bobSession := &types.Session{
		TenantID: 1,
		UserID:   "bob",
		Title:    "bob private session",
	}
	require.NoError(t, db.Create(bobSession).Error)
	legacySession := &types.Session{
		TenantID: 1,
		Title:    "legacy tenant session",
	}
	require.NoError(t, db.Create(legacySession).Error)

	_, err := svc.GetSession(testSessionScopeContext(1, "bob"), aliceSession.ID)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)

	got, err := svc.GetSession(testSessionScopeContext(1, "bob"), bobSession.ID)
	require.NoError(t, err)
	require.Equal(t, bobSession.ID, got.ID)

	got, err = svc.GetSession(testSessionScopeContext(1, "bob"), legacySession.ID)
	require.NoError(t, err)
	require.Equal(t, legacySession.ID, got.ID)
}

func TestUpdateSessionIsScopedToCurrentUserAndAllowsNoOp(t *testing.T) {
	svc, db := newTestSessionService(t)
	aliceSession := &types.Session{
		TenantID:    1,
		UserID:      "alice",
		Title:       "alice private session",
		Description: "original description",
	}
	require.NoError(t, db.Create(aliceSession).Error)

	err := svc.UpdateSession(testSessionScopeContext(1, "bob"), &types.Session{
		ID:          aliceSession.ID,
		TenantID:    1,
		Title:       "bob update attempt",
		Description: "should not be saved",
	})
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)

	var unchanged types.Session
	require.NoError(t, db.First(&unchanged, "id = ?", aliceSession.ID).Error)
	require.Equal(t, aliceSession.Title, unchanged.Title)
	require.Equal(t, aliceSession.Description, unchanged.Description)

	err = svc.UpdateSession(testSessionScopeContext(1, "alice"), &types.Session{
		ID:          aliceSession.ID,
		TenantID:    1,
		Title:       aliceSession.Title,
		Description: aliceSession.Description,
	})
	require.NoError(t, err)
}

func TestUpdateSessionRejectsPlantedMaintenanceDescription(t *testing.T) {
	svc, db := newTestSessionService(t)
	aliceSession := &types.Session{
		TenantID:    1,
		UserID:      "alice",
		Title:       "alice private session",
		Description: "ordinary",
	}
	require.NoError(t, db.Create(aliceSession).Error)

	err := svc.UpdateSession(testSessionScopeContext(1, "alice"), &types.Session{
		ID:          aliceSession.ID,
		TenantID:    1,
		Title:       "still mine",
		Description: types.SkillMaintenanceSessionMarker + "install",
	})
	require.NoError(t, err)

	var got types.Session
	require.NoError(t, db.First(&got, "id = ?", aliceSession.ID).Error)
	require.Equal(t, "still mine", got.Title)
	require.Empty(t, got.Description,
		"a client must not be able to hide a session behind the maintenance marker")
}

func TestUpdateSessionPreservesMaintenanceDescription(t *testing.T) {
	svc, db := newTestSessionService(t)
	marker := types.SkillMaintenanceSessionMarker + "install"
	row := &types.Session{
		TenantID:    1,
		UserID:      "alice",
		Title:       "Skill install",
		Description: marker,
	}
	require.NoError(t, db.Create(row).Error)

	err := svc.UpdateSession(testSessionScopeContext(1, "alice"), &types.Session{
		ID:          row.ID,
		TenantID:    1,
		Title:       "unhide me",
		Description: "plain chat",
	})
	require.NoError(t, err)

	var got types.Session
	require.NoError(t, db.First(&got, "id = ?", row.ID).Error)
	require.Equal(t, marker, got.Description,
		"a PUT must not strip the marker off a real maintenance session")
}

// testListSessionsIMChannelSession lets QueryPaged's LEFT JOIN resolve against a
// real table in the isolated PostgreSQL database.
type testListSessionsIMChannelSession struct {
	ID          uint64 `gorm:"primaryKey;autoIncrement"`
	SessionID   string `gorm:"column:session_id"`
	Platform    string `gorm:"column:platform"`
	ChatID      string `gorm:"column:chat_id"`
	ThreadID    string `gorm:"column:thread_id"`
	UserID      string `gorm:"column:user_id"`
	AgentID     string `gorm:"column:agent_id"`
	IMChannelID string `gorm:"column:im_channel_id"`
}

func (testListSessionsIMChannelSession) TableName() string { return "im_channel_sessions" }
