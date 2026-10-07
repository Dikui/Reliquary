package database

import (
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationScriptEncodesPasswordAndResetsBaseline(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("migration password encoding requires python3")
	}
	root := t.TempDir()
	scriptDir := filepath.Join(root, "scripts")
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.Mkdir(scriptDir, 0o700))
	require.NoError(t, os.Mkdir(binDir, 0o700))
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	source, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "migrate.sh"))
	require.NoError(t, err)
	script := filepath.Join(scriptDir, "migrate.sh")
	require.NoError(t, os.WriteFile(script, source, 0o700))
	// Capture only the subprocess arguments; the test never contacts a database.
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "migrate"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MIGRATE_CAPTURE\"\n"), 0o700))
	capture := filepath.Join(root, "arguments")
	password := "p'ass\" \\ @:/;$(literal)`literal`\nend"
	cmd := exec.Command("bash", script, "up")
	cmd.Env = []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"DB_PASSWORD=" + password, "DB_USER=test-user", "DB_NAME=test-db",
		"DB_HOST=localhost", "DB_PORT=5432", "DB_URL=",
		"MIGRATIONS_DIR=migrations with spaces", "MIGRATE_CAPTURE=" + capture,
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	arguments, err := os.ReadFile(capture)
	require.NoError(t, err)
	args := strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n")
	require.Len(t, args, 5)
	require.Equal(t, []string{"-path", "migrations with spaces", "-database"}, args[:3])
	require.Equal(t, "up", args[4])
	databaseURL, err := url.Parse(args[3])
	require.NoError(t, err)
	decodedPassword, ok := databaseURL.User.Password()
	require.True(t, ok)
	require.Equal(t, password, decodedPassword)
	require.NotContains(t, string(output), password)
	require.NotContains(t, string(output), args[3], "the script must not print credentials")

	force := exec.Command("bash", script, "force", "-1")
	force.Env = cmd.Env
	output, err = force.CombinedOutput()
	require.NoError(t, err, "%s", output)
	arguments, err = os.ReadFile(capture)
	require.NoError(t, err)
	forceArgs := strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n")
	require.Equal(t, append(args[:4], "force", "--", "-1"), forceArgs)
	require.NotContains(t, string(output), password)
	require.NotContains(t, string(output), args[3], "the script must not print credentials")
}
