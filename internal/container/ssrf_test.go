package container

import (
	"os"
	"testing"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/utils"
)

func TestMain(m *testing.M) {
	// Engine wiring tests intentionally use loopback httptest servers.
	utils.SetSSRFWhitelistFromRaw("127.0.0.1,::1,localhost")
	code := pgtest.Run(m)
	utils.SetSSRFWhitelistFromRaw("")
	os.Exit(code)
}
