package container

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeDuckDBDisablesExtensionDownloads(t *testing.T) {
	t.Setenv("DUCKDB_SKIP_EXTENSION_LOAD", "1")
	database, err := NewDuckDB()
	require.NoError(t, err)
	defer database.Close()
	var install, load bool
	require.NoError(t, database.QueryRow("SELECT current_setting('autoinstall_known_extensions'), current_setting('autoload_known_extensions')").Scan(&install, &load))
	require.False(t, install, "parsing a file cannot automatically download an extension")
	require.False(t, load, "parsing uses only extensions already loaded at startup")
}
