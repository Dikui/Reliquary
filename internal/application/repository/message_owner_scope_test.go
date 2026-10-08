package repository

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// These tests preserve the repository boundary between session owners when
// searching stored conversations.

// Session.BeforeCreate assigns a fresh UUID, so the ids are read back rather
// than assumed.
func newOwnerScopeDB(t *testing.T) (*gorm.DB, map[string]string) {
	t.Helper()
	db := pgtest.OpenFixture(t)
	if err := db.AutoMigrate(&types.Session{}, &types.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sessions := map[string]*types.Session{
		"alice":  {TenantID: 7, UserID: "web_user:alice", Title: "Alice 的会话"},
		"bob":    {TenantID: 7, UserID: "web_user:bob", Title: "Bob 的会话"},
		"legacy": {TenantID: 7, Title: "API 建的会话"},
		"other":  {TenantID: 8, UserID: "web_user:alice", Title: "别的工作区"},
	}
	ids := make(map[string]string, len(sessions))
	for label, session := range sessions {
		if err := db.Create(session).Error; err != nil {
			t.Fatalf("insert session: %v", err)
		}
		ids[label] = session.ID
	}
	return db, ids
}

func TestOwnedSessionIDsExcludesOtherPeople(t *testing.T) {
	db, ids := newOwnerScopeDB(t)
	repo := NewMessageRepository(db)

	owned, err := repo.OwnedSessionIDs(context.Background(), 7, "web_user:alice",
		[]string{ids["alice"], ids["bob"], ids["legacy"], ids["other"]})
	if err != nil {
		t.Fatalf("owned session ids: %v", err)
	}

	if !owned[ids["alice"]] {
		t.Error("alice must be able to search her own conversations")
	}
	if owned[ids["bob"]] {
		t.Error("alice must not be able to reach bob's conversations")
	}
	if !owned[ids["legacy"]] {
		t.Error("tenant-level sessions stay reachable, matching how they are listed")
	}
	if owned[ids["other"]] {
		t.Error("a workspace boundary is not something an owner check may cross")
	}
}

func TestOwnedSessionIDsWithoutAnOwnerKeepsWorkspaceScope(t *testing.T) {
	db, ids := newOwnerScopeDB(t)
	repo := NewMessageRepository(db)

	// An empty owner means "no per-user narrowing", which is what admin-console
	// style callers rely on. It must still not leak across workspaces.
	owned, err := repo.OwnedSessionIDs(context.Background(), 7, "",
		[]string{ids["alice"], ids["bob"], ids["other"]})
	if err != nil {
		t.Fatalf("owned session ids: %v", err)
	}
	if !owned[ids["alice"]] || !owned[ids["bob"]] {
		t.Error("without an owner filter, every session in the workspace qualifies")
	}
	if owned[ids["other"]] {
		t.Error("tenant scoping is not optional")
	}
}
