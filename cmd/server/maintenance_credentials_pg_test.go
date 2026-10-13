package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"reliquary/internal/testutil/pgtest"

	"github.com/stretchr/testify/require"
)

func TestOfflineCredentialsRejectMissingEncryptionKeyAndReadableInputFile(t *testing.T) {
	db := pgtest.Open(t)
	t.Setenv("SYSTEM_AES_KEY", "")
	var output bytes.Buffer
	err := runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture"}, strings.NewReader(`{"api_key":"fixture-secret"}`), &output)
	require.ErrorContains(t, err, "32-byte")
	require.Empty(t, output.String())
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	input := t.TempDir() + "/credentials.json"
	require.NoError(t, os.WriteFile(input, []byte(`{"api_key":"fixture-secret"}`), 0644))
	err = runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture", "--input", input}, bytes.NewReader(nil), &output)
	require.ErrorContains(t, err, "owner-only")
	require.NotContains(t, err.Error(), "fixture-secret")
	err = runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture"}, strings.NewReader(`{"api_key":"fixture-secret"} {}`), &output)
	require.ErrorContains(t, err, "exactly one")
	err = runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture"}, strings.NewReader(strings.Repeat(" ", 1<<20)+"{}"), &output)
	require.ErrorContains(t, err, "1 MiB")
}
