package pgtest

import (
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// MigrationVersions follows the checked-in migration files, including reserved
// gaps, so adding a migration does not weaken the existing version assertions.
func MigrationVersions(t testing.TB) []uint {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(source), "../../../migrations/versioned/*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	versions := []uint{}
	for _, file := range files {
		prefix, _, ok := strings.Cut(filepath.Base(file), "_")
		if !ok {
			t.Fatalf("invalid migration filename %s", file)
		}
		version, err := strconv.ParseUint(prefix, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, uint(version))
	}
	slices.Sort(versions)
	if len(versions) == 0 {
		t.Fatal("no versioned migrations")
	}
	return versions
}
func LatestMigrationVersion(t testing.TB) uint {
	t.Helper()
	versions := MigrationVersions(t)
	return versions[len(versions)-1]
}
