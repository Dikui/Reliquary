package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

func TestLegacyReplicasImportCurrentRowsAndPreserveOpaqueValues(t *testing.T) {
	for _, format := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			ctx := context.Background()
			source := pgtest.Open(t)
			seed(t, source)
			require.NoError(t, source.Exec(`UPDATE entries SET custom_metadata='{"tenant_id":"keep opaque"}'::jsonb WHERE id='article'; UPDATE task_jobs SET payload='{"tenant_id":1,"entry_id":"article"}'::jsonb`).Error)
			snapshot, err := Capture(ctx, source)
			require.NoError(t, err)
			root := t.TempDir()
			require.NoError(t, WriteSnapshot(root, "", snapshot))
			path := filepath.Join(root, ".reliquary", "manifest.json")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var manifest ReplicaManifest
			require.NoError(t, json.Unmarshal(data, &manifest))
			manifest.FormatVersion = format
			for i, table := range manifest.Tables {
				manifest.Tables[i] = legacyTable(table, format)
			}
			for i := range manifest.Required {
				manifest.Required[i].Kind = legacyTable(manifest.Required[i].Kind, format)
			}
			nulls := map[string]map[string][]string{}
			for table, values := range manifest.JSONNulls {
				nulls[legacyTable(table, format)] = values
			}
			manifest.JSONNulls = nulls
			for i, file := range manifest.Files {
				if strings.HasPrefix(file.Path, "config/") || strings.HasPrefix(file.Path, "data/") {
					if file.Path == "config/required_credentials.json" {
						payload, err := json.Marshal(manifest.Required)
						require.NoError(t, err)
						contents, err := json.MarshalIndent(portableRequirementCopy{format, manifest.Required, sum(payload)}, "", "  ")
						require.NoError(t, err)
						contents = append(contents, '\n')
						require.NoError(t, AtomicWrite(filepath.Join(root, ".reliquary", file.Path), contents))
						manifest.Files[i] = File{Path: file.Path, SHA256: sum(contents), Size: int64(len(contents))}
						continue
					}
					table := strings.TrimSuffix(filepath.Base(file.Path), ".json")
					rows := namedRows(snapshot.Tables[table], true, format)
					contents, err := json.MarshalIndent(rows, "", "  ")
					require.NoError(t, err)
					contents = append(contents, '\n')
					oldPath := filepath.Join(filepath.Dir(file.Path), legacyTable(table, format)+".json")
					require.NoError(t, AtomicWrite(filepath.Join(root, ".reliquary", oldPath), contents))
					if oldPath != file.Path {
						require.NoError(t, os.Remove(filepath.Join(root, ".reliquary", file.Path)))
					}
					manifest.Files[i] = File{Path: oldPath, SHA256: sum(contents), Size: int64(len(contents))}
				}
			}
			data, err = json.Marshal(manifest)
			require.NoError(t, err)
			require.NoError(t, AtomicWrite(path, data))
			loaded, err := Load(root)
			require.NoError(t, err)
			require.Equal(t, FormatVersion, loaded.FormatVersion)
			require.Contains(t, loaded.Tables, "spaces")
			require.Contains(t, loaded.Tables, "api_keys")
			verification, err := VerifyReplica(ctx, source, root, "", false)
			require.NoError(t, err)
			require.True(t, verification.OK, verification.Differences)
			target := pgtest.Open(t)
			_, err = Import(ctx, target, root, "", nil)
			require.NoError(t, err)
			require.NoError(t, VerifyImportedReplica(ctx, target, root))
			var meta string
			require.NoError(t, target.Raw(`SELECT custom_metadata::text FROM entries WHERE id='article'`).Scan(&meta).Error)
			require.JSONEq(t, `{"tenant_id":"keep opaque"}`, meta)
			newSnapshot, err := Capture(ctx, target)
			require.NoError(t, err)
			newRoot := t.TempDir()
			require.NoError(t, WriteSnapshot(newRoot, "", newSnapshot))
			third := pgtest.Open(t)
			_, err = Import(ctx, third, newRoot, "", nil)
			require.NoError(t, err)
			require.NoError(t, VerifyImportedReplica(ctx, third, newRoot))
			newLoaded, err := Load(newRoot)
			require.NoError(t, err)
			require.Equal(t, FormatVersion, newLoaded.FormatVersion)
		})
	}
}
