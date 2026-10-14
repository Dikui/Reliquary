package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
)

func TestMaintenanceRestoredRelationsRebuildNeo4j(t *testing.T) {
	if os.Getenv("RELIQUARY_TEST_NEO4J_URI") == "" {
		t.Skip("set RELIQUARY_TEST_NEO4J_URI for restored graph maintenance test")
	}
	db := pgtest.Open(t)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Graph restore','personal');
 INSERT INTO libraries(id,space_id,name,template,directory,system_role,embedding_model_id,summary_model_id,chunking_config,indexing_strategy) VALUES('library',1,'Mine','own','mine','mine','','','{}','{}');
 INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at) VALUES('a',1,'A','a','company',now(),now()),('b',1,'B','b','company',now(),now());
 INSERT INTO entries(id,space_id,library_id,title,type,source,parse_status,enable_status,created_at,updated_at) VALUES('source',1,'library','Source','manual','manual','completed','enabled',now(),now());
 INSERT INTO library_relation_types(library_id,name,position,enabled) VALUES('library','cooperates',0,true);
 INSERT INTO relations(id,space_id,library_id,subject_id,relation_type,object_id,version) VALUES('r',1,'library','a','cooperates','b',3);
 INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash) VALUES('r','source','[]','run','hash');`).Error)
	snapshot, err := maintenance.Capture(context.Background(), db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(root, "", snapshot))
	restored := pgtest.Open(t)
	_, err = maintenance.Import(context.Background(), restored, root, "", nil)
	require.NoError(t, err)
	connection, err := url.Parse(restored.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	password, _ := connection.User.Password()
	for key, value := range map[string]string{"DB_DRIVER": "postgres", "DB_HOST": connection.Hostname(), "DB_PORT": connection.Port(), "DB_USER": connection.User.Username(), "DB_PASSWORD": password, "DB_NAME": connection.Path[1:], "VAULT_ROOT": root, "NEO4J_URI": os.Getenv("RELIQUARY_TEST_NEO4J_URI"), "NEO4J_USERNAME": os.Getenv("RELIQUARY_TEST_NEO4J_USERNAME"), "NEO4J_PASSWORD": os.Getenv("RELIQUARY_TEST_NEO4J_PASSWORD")} {
		t.Setenv(key, value)
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(repo)
	driver, err := neo4j.NewDriver(os.Getenv("NEO4J_URI"), neo4j.BasicAuth(os.Getenv("NEO4J_USERNAME"), os.Getenv("NEO4J_PASSWORD"), ""))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := neo4j.ExecuteQuery(context.Background(), driver, "MATCH (n) DETACH DELETE n", nil, neo4j.EagerResultTransformer)
		require.NoError(t, err)
		require.NoError(t, driver.Close(context.Background()))
	})
	var output bytes.Buffer
	require.NoError(t, runMaintenance([]string{"graph-rebuild"}, bytes.NewReader(nil), &output))
	var result map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.EqualValues(t, 1, result["pg_edges"])
	require.Equal(t, result["pg_hash"], result["neo4j_hash"])
	actual, err := neo4j.ExecuteQuery(context.Background(), driver, "MATCH (:Entity {id:'a'})-[r:RELATION]->(:Entity {id:'b'}) RETURN r.version,r.source_count", nil, neo4j.EagerResultTransformer)
	require.NoError(t, err)
	require.Len(t, actual.Records, 1)
	require.EqualValues(t, 3, actual.Records[0].Values[0])
	require.EqualValues(t, 1, actual.Records[0].Values[1])
	output.Reset()
	require.NoError(t, runMaintenance([]string{"graph-check"}, bytes.NewReader(nil), &output))
	require.Contains(t, output.String(), `"repaired": false`)
}
