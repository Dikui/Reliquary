package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestPersonalRegistrationSerializesFirstUser(t *testing.T) {
	db := pgtest.Open(t)
	if err := db.Create(&types.Space{ID: types.SingleSpaceID, Name: "Team", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewUserRepository(db)
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := &types.User{ID: fmt.Sprintf("user-%d", i), Username: fmt.Sprintf("user-%d", i), Email: fmt.Sprintf("user-%d@team.test", i), IsActive: true, SpaceID: types.SingleSpaceID}
			results <- repo.RegisterSingleUser(context.Background(), user)
		}(i)
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrRegistrationClosed) {
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
	if len(users) != 1 || users[0].SpaceID != types.SingleSpaceID {
		t.Fatalf("unexpected users=%+v", users)
	}
	if err := repo.RegisterSingleUser(context.Background(), &types.User{ID: "second", Username: "second", Email: "second@test.local"}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("second registration=%v", err)
	}
	if err := db.Create(&types.Space{ID: 2, Name: "Second"}).Error; err == nil {
		t.Fatal("database allowed a second space")
	}
}
