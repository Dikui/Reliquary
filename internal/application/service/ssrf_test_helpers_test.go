package service

import (
	"reliquary/internal/utils"
	"testing"
)

// withSSRFWhitelist isolates the shared policy cache for HTTP integration tests.
func withSSRFWhitelist(t *testing.T, whitelist string) {
	t.Helper()
	utils.ResetSSRFWhitelistForTest()
	t.Setenv("SSRF_WHITELIST", whitelist)
	t.Cleanup(func() {
		utils.ResetSSRFWhitelistForTest()
	})
}
