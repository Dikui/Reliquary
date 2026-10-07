package service

import (
	"os"
	"testing"

	"reliquary/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m))
}
