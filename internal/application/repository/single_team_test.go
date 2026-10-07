package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestTeamRegistrationSerializesFirstOwner(t *testing.T) {
	db := pgtest.Open(t)
	if err := db.Create(&types.Tenant{ID: types.SingleTenantID, Name: "Team", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewUserRepository(db)
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := &types.User{ID: fmt.Sprintf("user-%d", i), Username: fmt.Sprintf("user-%d", i), Email: fmt.Sprintf("user-%d@team.test", i), IsActive: true, TenantID: types.SingleTenantID}
			results <- repo.RegisterTeamUser(context.Background(), user, "")
		}(i)
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrTeamInvitationRequired) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("registered %d first users, want 1", succeeded)
	}
	var users []types.User
	if err := db.Find(&users).Error; err != nil {
		t.Fatal(err)
	}
	var members []types.TenantMember
	if err := db.Find(&members).Error; err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || !users[0].IsSystemAdmin || users[0].TenantID != types.SingleTenantID || len(members) != 1 || members[0].Role != types.TenantRoleOwner {
		t.Fatalf("unexpected bootstrap users=%+v members=%+v", users, members)
	}
}

func TestTeamInvitationRegistrationIsAtomic(t *testing.T) {
	db := pgtest.Open(t)
	if err := db.Create(&types.Tenant{ID: types.SingleTenantID, Name: "Team", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewUserRepository(db)
	ctx := context.Background()
	owner := &types.User{ID: "owner", Username: "owner", Email: "owner@team.test", IsActive: true, TenantID: types.SingleTenantID}
	if err := repo.RegisterTeamUser(ctx, owner, ""); err != nil {
		t.Fatal(err)
	}
	inv := &types.TenantInvitation{TenantID: types.SingleTenantID, Token: "team-invite", Role: types.TenantRoleContributor, Status: types.TenantInvitationStatusPending, ExpiresAt: time.Now().Add(time.Hour), InvitedBy: &owner.ID}
	if err := db.Create(inv).Error; err != nil {
		t.Fatal(err)
	}
	user := &types.User{ID: "invitee", Username: "invitee", Email: "invitee@team.test", IsActive: true}
	if err := repo.RegisterTeamUser(ctx, user, inv.Token); err != nil {
		t.Fatal(err)
	}
	var member types.TenantMember
	if err := db.Where("user_id = ?", user.ID).First(&member).Error; err != nil {
		t.Fatal(err)
	}
	if user.TenantID != owner.TenantID || user.IsSystemAdmin || member.Role != types.TenantRoleContributor {
		t.Fatalf("unexpected invited user=%+v member=%+v", user, member)
	}
	if err := repo.RegisterTeamUser(ctx, &types.User{ID: "bad", Username: "bad", Email: "bad@team.test"}, "revoked"); !errors.Is(err, ErrInvalidTeamInvitation) {
		t.Fatalf("invalid invitation: %v", err)
	}
	if err := repo.RegisterTeamUser(ctx, &types.User{ID: "duplicate", Username: "invitee", Email: "duplicate@team.test"}, inv.Token); err == nil {
		t.Fatal("duplicate identity succeeded")
	}
	if err := db.First(inv, inv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if inv.AcceptedCount != 1 {
		t.Fatalf("failed registration consumed invitation: count=%d", inv.AcceptedCount)
	}
	var count int64
	if err := db.Model(&types.User{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("partial user remained: count=%d err=%v", count, err)
	}
	if err := db.Create(&types.Tenant{ID: 2, Name: "Second"}).Error; err == nil {
		t.Fatal("database allowed a second tenant")
	}
}
