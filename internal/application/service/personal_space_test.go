package service

import (
	"context"
	"testing"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestEnsurePersonalSpaceIsIdempotent(t *testing.T) {
	db := pgtest.Open(t)
	for i := 0; i < 2; i++ {
		if err := EnsurePersonalSpace(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var spaces []types.Space
	if err := db.Find(&spaces).Error; err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 1 || spaces[0].ID != types.SingleSpaceID {
		t.Fatalf("team bootstrap=%+v", spaces)
	}
}
