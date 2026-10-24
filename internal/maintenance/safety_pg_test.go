package maintenance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/utils"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVaultOnlyLoadRejectsTamperedBodyAndMissingTableBeforeImport(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	root := t.TempDir()
	article := filepath.Join(root, "articles/original.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(article), 0o700))
	require.NoError(t, os.WriteFile(article, []byte("Original body and unchanged ID"), 0o600))
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	_, err = Load(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(article, []byte("Modified body and unchanged ID"), 0o600))
	_, err = Load(root)
	require.ErrorContains(t, err, "vault file checksum mismatch")
	require.NoError(t, os.WriteFile(article, []byte("Original body and unchanged ID"), 0o600))
	var manifest ReplicaManifest
	manifestPath := filepath.Join(root, ".reliquary/manifest.json")
	require.NoError(t, json.Unmarshal(mustRead(t, manifestPath), &manifest))
	filtered := []File{}
	for _, file := range manifest.Files {
		if file.Path != "data/entities.json" {
			filtered = append(filtered, file)
		}
	}
	manifest.Files = filtered
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, data, 0o600))
	_, err = Load(root)
	require.ErrorContains(t, err, "missing replica table entities")
}

func TestReplicaRefusesCredentialsInUploadedAssetWithoutChangingOriginal(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	root, uploads := t.TempDir(), t.TempDir()
	original := []byte("Original binary-compatible source contains MODEL_SECRET and must remain untouched.")
	path := filepath.Join(uploads, "raw-original.txt")
	require.NoError(t, os.WriteFile(path, original, 0o600))
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	err = WriteSnapshot(root, uploads, snapshot)
	require.ErrorContains(t, err, "replica refused")
	require.NotContains(t, err.Error(), "MODEL_SECRET")
	require.Equal(t, original, mustRead(t, path))
	_, statErr := os.Stat(filepath.Join(root, ".reliquary/resources/raw-original.txt"))
	require.True(t, os.IsNotExist(statErr))
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "MODEL_SECRET")
}

func TestImportRejectsNewPersistentTableWithoutDependencyOrder(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	Rules["new_metadata_table"] = Rule{Sidecar: "id description"}
	t.Cleanup(func() { delete(Rules, "new_metadata_table") })
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	snapshot.Tables["new_metadata_table"] = []map[string]any{{"id": "new", "description": "must not silently disappear"}}
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	_, err = Import(ctx, db, root, "", nil)
	require.ErrorContains(t, err, "new_metadata_table has no import dependency order")
}

func TestOriginalPublicURLExamplesRemainExactWhileConfigurationCredentialsAreExcluded(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	example := "https://example.test/linguistics?token=public-lexical-example&api_key=example-placeholder"
	require.NoError(t, db.Exec(`UPDATE entries SET vault_body=?,metadata=?::jsonb WHERE id='pending'`, example, `{"manual":{"content":"`+example+`"},"intake":{"original_content":"`+example+`"}}`).Error)
	require.NoError(t, db.Exec(`UPDATE models SET parameters=parameters || '{"base_url":"https://provider.example.test?api_key=external-private-key","extra_config":{"access_key_id":"storage-access-private-12345","secret_access_key":"storage-secret-private-12345","secret_key":"storage-provider-secret-12345"}}'::jsonb WHERE id='chat'`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, row := range snapshot.Tables["entries"] {
		if row["id"] != "pending" {
			continue
		}
		require.Equal(t, example, row["vault_body"])
		metadata := row["metadata"].(map[string]any)
		require.Equal(t, example, metadata["manual"].(map[string]any)["content"])
		require.Equal(t, example, metadata["intake"].(map[string]any)["original_content"])
	}
	for _, secret := range []string{"external-private-key", "storage-access-private-12345", "storage-secret-private-12345", "storage-provider-secret-12345"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Contains(t, string(encoded), "parameters.extra_config.access_key_id")
	require.Contains(t, string(encoded), "parameters.base_url.query.api_key")
}

func TestVectorDimensionsMatchActualValuesAndModelConfiguration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	for _, statement := range []string{
		`INSERT INTO chunks(id,space_id,library_id,entry_id,content,chunk_index,start_at,end_at,content_hash) VALUES('dim-chunk',1,'library','article','observations',0,0,12,'chunkhash')`,
		`INSERT INTO embeddings(source_id,source_type,chunk_id,entry_id,library_id,model_id,dimension,embedding) VALUES('dim-chunk',0,'dim-chunk','article','library','embed',3,'[1,0,0]')`,
		`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash) VALUES('article',1,'library','embed',3,'[1,0,0]','articlehash')`,
		`INSERT INTO image_embeddings(entry_id,space_id,library_id,attachment_path,model_id,dimension,embedding,content_hash) VALUES('article',1,'library','image.png','embed',3,'[1,0,0]','imagehash')`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	differences, err := CheckVectorDimensions(ctx, db)
	require.NoError(t, err)
	require.Empty(t, differences)
	for table, column := range map[string]string{
		"embeddings":         "dimension",
		"article_embeddings": "dimension",
		"image_embeddings":   "dimension",
	} {
		vectorColumn := "embedding"
		require.NoError(t,
			db.Exec("UPDATE "+table+" SET "+column+"=4,"+vectorColumn+"='[1,0,0,0]' WHERE "+column+"=3").Error)
		differences, err = CheckVectorDimensions(ctx, db)
		require.NoError(t, err)
		require.Contains(t, strings.Join(differences, ";"), table)
		require.NoError(t, db.Exec("UPDATE "+table+" SET "+column+"=3,"+vectorColumn+"='[1,0,0]' WHERE "+column+"=4").Error)
	}
	// The article and image tables already reject a declared/actual mismatch.
	// Legacy chunk vectors also need the maintenance check.
	for table, column := range map[string]string{"embeddings": "dimension"} {
		require.NoError(t, db.Table(table).Where(column+"=3").UpdateColumn(column, 4).Error)
		differences, err = CheckVectorDimensions(ctx, db)
		require.NoError(t, err)
		require.Contains(t, strings.Join(differences, ";"), table)
		require.NoError(t, db.Table(table).Where(column+"=4").UpdateColumn(column, 3).Error)
	}
	require.NoError(t, db.Exec(`UPDATE models SET parameters=jsonb_set(parameters,'{embedding_parameters}','{"dimension":4}'::jsonb) WHERE id='embed'`).Error)
	differences, err = CheckVectorDimensions(ctx, db)
	require.NoError(t, err)
	require.Len(t, differences, 3)
}

func TestGitBundleCredentialScanReadsCompressedHistoryObjects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	bundle := filepath.Join(t.TempDir(), "code.bundle")
	command := exec.Command("git", "bundle", "create", bundle, "--all")
	require.NoError(t, command.Run())
	sentinel := make([]byte, 32)
	_, err := rand.Read(sentinel)
	require.NoError(t, err)
	require.NoError(t, scanGitBundle(context.Background(), bundle, []string{hex.EncodeToString(sentinel)}))
	// The project name occurs in committed files, proving that packed objects
	// are decoded rather than searching only the compressed bundle bytes.
	require.Error(t, scanGitBundle(context.Background(), bundle, []string{"Reliquary"}))
}

func TestCredentialsStayInMemoryAndAreExcludedFromHeadersErrorsAndCompressedFiles(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	key := []byte("12345678901234567890123456789012")
	t.Setenv("SYSTEM_AES_KEY", string(key))
	t.Setenv("JWT_SECRET", "owner-jwt-operational-secret-9876543210")
	secret := "current-private-provider-token-1234567890"
	ciphertext, err := utils.EncryptAESGCM(secret, key)
	require.NoError(t, err)
	chatgptCredentials, err := utils.EncryptAESGCM(`{"access_token":"chatgpt-access-private-876543210","refresh_token":"chatgpt-refresh-private-876543210","token_type":"Bearer","scope":"openid","username":"public-fixture-user","email":"public@example.test","client_id":"public-client"}`, key)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE chatgpt_accounts SET credentials=?`, chatgptCredentials).Error)
	require.NoError(t, db.Exec(`UPDATE models SET parameters=jsonb_set(parameters,'{api_key}',to_jsonb(?::text)) WHERE id='chat'`, ciphertext).Error)
	require.NoError(t, db.Exec(`UPDATE models SET parameters=jsonb_set(parameters,'{custom_headers}','{"X-Auth":"unusual-authentication-value-1234567890","Accept":"application/json","Content-Type":"application/json","User-Agent":"Reliquary"}'::jsonb) WHERE id='chat'`).Error)
	require.NoError(t, db.Exec(`UPDATE entries SET error_message=?,vault_body=?,metadata='{"original":"Bearer tokens authenticate HTTP requests."}'::jsonb WHERE id='pending'`, "Provider repeated "+secret+" during retry; owner-jwt-operational-secret-9876543210", "Basic principles are preserved in the original. "+secret).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret)
	require.NotContains(t, string(encoded), ciphertext)
	require.NotContains(t, string(encoded), "unusual-authentication-value-1234567890")
	require.Contains(t, string(encoded), "Provider repeated [credential excluded] during retry")
	require.NotContains(t, string(encoded), "owner-jwt-operational-secret-9876543210")
	require.Contains(t, string(encoded), "Basic principles are preserved in the original.")
	require.Contains(t, string(encoded), "Bearer tokens authenticate HTTP requests.")
	require.Contains(t, string(encoded), "parameters.custom_headers.X-Auth")
	require.Contains(t, string(encoded), "application/json")
	require.NoError(t, ScanCredentialReader(ctx, db, strings.NewReader("safe database dump Bearer openid public-fixture-user public@example.test public-client")))
	require.Error(t, ScanCredentialReader(ctx, db, strings.NewReader("chatgpt-refresh-private-876543210")))
	err = ScanCredentialReader(ctx, db, strings.NewReader("diagnostic contains "+secret))
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
	root := t.TempDir()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write([]byte(strings.Repeat("Layer data "+secret+" ", 100)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	var archive bytes.Buffer
	tarWriter := tar.NewWriter(&archive)
	require.NoError(
		t,
		tarWriter.WriteHeader(
			&tar.Header{Name: "layer.tar.gz", Mode: 0o600, Size: int64(compressed.Len())},
		),
	)
	_, err = tarWriter.Write(compressed.Bytes())
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NotContains(t, string(archive.Bytes()), secret)
	require.NoError(t, os.WriteFile(filepath.Join(root, "image.tar"), archive.Bytes(), 0o600))
	err = ScanCredentialFiles(ctx, db, root)
	require.ErrorContains(t, err, "image.tar")
	require.NotContains(t, err.Error(), secret)
	require.NoError(t, os.Remove(filepath.Join(root, "image.tar")))
	require.NoError(t, os.WriteFile(filepath.Join(root, secret+".txt"), []byte("ordinary bytes"), 0o600))
	err = ScanCredentialFiles(ctx, db, root)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
	_, err = Scrub(ctx, db)
	require.NoError(t, err)
	var diagnostic string
	require.NoError(t, db.Table("entries").Select("error_message").Where("id='pending'").Scan(&diagnostic).Error)
	require.Equal(t, "Provider repeated [credential excluded] during retry; [credential excluded]", diagnostic)
}

func TestManifestExplicitModelChangeAndLogAppendStillRejectPermanentDataTampering(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	root, uploads := t.TempDir(), t.TempDir()
	expected, err := BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	data, err := json.Marshal(expected)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, expected))
	require.NoError(t, db.Exec(`INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('new-configuration-log',1,'owner','update_model','embed','null'::jsonb,'{"changed":true}')`).Error)
	actual, err := BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table operation_logs")
	require.Empty(t, CompareManifestWithOptions(expected, actual, false, true))
	require.NoError(t, db.Exec(`INSERT INTO models(id,space_id,name,type,source,parameters,status) VALUES('new-embed',1,'new embedding model','Embedding','remote','{"dimension":4,"base_url":"https://embedding.example.test"}','active')`).Error)
	require.NoError(t, db.Exec(`UPDATE libraries SET embedding_model_id='new-embed',model_tiers=jsonb_set(model_tiers,'{embedding}','"new-embed"'),updated_at=now() WHERE id='library'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embedding_rebuild_runs(id,space_id,model_id,model_name,dimension,profile_hash,status) VALUES('new-rebuild',1,'new-embed','new embedding model',4,'new-profile','completed')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO embedding_rebuild_items(id,run_id,kind,source_id,status) VALUES('new-rebuild-item','new-rebuild','article','article','completed')`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table models")
	require.Empty(t, CompareManifestWithOptions(expected, actual, true, true))
	appendExpected := *actual
	appendExpected.Tables = map[string]TableManifest{}
	for table, entry := range actual.Tables {
		appendExpected.Tables[table] = entry
	}
	require.NoError(t, db.Exec(`UPDATE embedding_rebuild_runs SET total_count=1 WHERE id='new-rebuild'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithOptions(&appendExpected, actual, true, true), "table embedding_rebuild_runs")
	require.NoError(t, db.Exec(`UPDATE embedding_rebuild_runs SET total_count=0,model_id='chat' WHERE id='new-rebuild'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithOptions(expected, actual, true, true), "table embedding_rebuild_runs")
	require.NoError(t, db.Exec(`UPDATE embedding_rebuild_runs SET model_id='new-embed' WHERE id='new-rebuild'`).Error)
	require.NoError(t, db.Exec(`UPDATE entries SET source_unverified=false WHERE id='article'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithOptions(expected, actual, true, true), "table entries")
	require.NoError(t, db.Exec(`UPDATE entries SET source_unverified=true WHERE id='article'`).Error)
	require.NoError(t, db.Exec(`UPDATE models SET display_name='changed unrelated chat model' WHERE id='chat'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithOptions(expected, actual, true, true), "table models")
	require.NoError(t, db.Exec(`UPDATE models SET display_name='' WHERE id='chat'`).Error)
	require.NoError(t, db.Exec(`UPDATE operation_logs SET actor='changed original actor' WHERE id='op-1'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithOptions(expected, actual, true, true), "table operation_logs")
	require.NoError(t, db.Exec(`DELETE FROM operation_logs WHERE id='op-1'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithOptions(expected, actual, true, true), "table operation_logs")
}
