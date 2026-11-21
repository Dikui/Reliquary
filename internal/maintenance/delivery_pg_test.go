package maintenance

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
)

func cloneDeliveryTestDatabase(t *testing.T, admin, source *gorm.DB) *gorm.DB {
	t.Helper()
	pool, err := source.DB()
	require.NoError(t, err)
	pool.SetMaxIdleConns(0) // PostgreSQL forbids cloning an active template.
	var sourceName string
	require.NoError(t, source.Raw("SELECT current_database()").Scan(&sourceName).Error)
	name := "delivery_test_" + uuid.NewString()
	require.NoError(t, admin.Exec("CREATE DATABASE "+quote(name)+" TEMPLATE "+quote(sourceName)).Error)
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + quote(name) + " WITH (FORCE)").Error; err != nil {
			t.Errorf("drop delivery test database: %v", err)
		}
	})
	dsn, err := url.Parse(source.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	dsn.Path = "/" + name
	db, err := gorm.Open(postgres.Open(dsn.String()))
	require.NoError(t, err)
	clonedPool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, clonedPool.Close()) })
	return db
}

func TestDeliveryCloneRemovesIdentityAndHistoryFromRestoredSnapshot(t *testing.T) {
	ctx := context.Background()
	source, admin := pgtest.Open(t), pgtest.Open(t)
	seed(t, source)
	for _, statement := range []string{
		`UPDATE users SET created_at='2026-01-01T00:00:00Z' WHERE id='owner'`,
		`INSERT INTO users(id,username,email,password_hash,space_id,created_at) VALUES('extra','extra','extra@example.test','extra-hash',1,'2026-02-01T00:00:00Z')`,
		`INSERT INTO auth_tokens(id,user_id,token,token_type,expires_at) VALUES('old-token','extra','TOKEN_SECRET','bearer',now())`,
		`INSERT INTO resource_access_grants(id,token_hash,resource_id,expires_at) VALUES('old-grant','GRANT_SECRET','resource',now())`,
		`INSERT INTO chatgpt_login_attempts(state,nonce,verifier,callback_url,client_id,account_id,expires_at)
		 VALUES('OAUTH_STATE','OAUTH_NONCE','OAUTH_VERIFIER','http://127.0.0.1/callback','client-preserved','00000000-0000-0000-0000-000000000001',now())`,
		`INSERT INTO models(id,space_id,name,type,source,parameters) VALUES('subscription',1,'catalogue-model','KnowledgeQA','chatgpt',
		 '{"provider":"chatgpt","interface_type":"responses","extra_config":{"chatgpt_account_id":"00000000-0000-0000-0000-000000000001"}}')`,
		`UPDATE libraries SET summary_model_id='subscription',model_tiers='{"generation":"subscription"}' WHERE id='library'`,
		`INSERT INTO chatgpt_policy(id,daily_limit,daily_used,day,paused,reason,retry_at,fallback_model_id) VALUES(1,10,5,'2026-10-11',true,'old quota reason',now(),'subscription')`,
		`UPDATE entries SET ingest_api_key_id=1, ingest_api_key_name='external caller', last_ingest_api_key_id=1, last_ingest_api_key_name='external caller' WHERE id='article'`,
		`INSERT INTO model_calls(id,space_id,requested_at,model_id,model_name,provider,purpose,currency,cost_status,duration_ms,success,error_category,attempt)
		 VALUES('00000000-0000-0000-0000-000000000010',1,now(),'subscription','catalogue-model','chatgpt','other','USD','unknown',1,true,'',1)`,
		`INSERT INTO model_call_contents(id,request_gzip,response_gzip,request_bytes,response_bytes,response_type,http_status,complete)
		 VALUES('00000000-0000-0000-0000-000000000010',decode('00','hex'),decode('00','hex'),1,1,'json',200,true)`,
		`INSERT INTO model_call_content_deletions(space_id,entry_id) VALUES(1,'article')`,
	} {
		require.NoError(t, source.Exec(statement).Error)
	}
	root := t.TempDir()
	before, err := BuildRuntimeManifest(ctx, source, root, "")
	require.NoError(t, err)
	clone := cloneDeliveryTestDatabase(t, admin, source)
	_, err = Scrub(ctx, clone)
	require.NoError(t, err)
	require.NoError(t, PrepareDelivery(ctx, clone))
	snapshot, err := BuildRuntimeManifest(ctx, clone, root, "")
	require.NoError(t, err)
	snapshot.Delivery = true
	for _, table := range []string{"chatgpt_accounts", "chatgpt_login_attempts", "chatgpt_hosts", "api_keys", "auth_tokens", "resource_access_grants", "operation_logs", "model_calls", "model_call_contents", "model_call_content_deletions"} {
		require.EqualValues(t, 1, before.Tables[table].Rows, table)
		require.Zero(t, snapshot.Tables[table].Rows, table)
		for _, requirement := range snapshot.Required {
			require.NotEqual(t, table, requirement.Kind)
		}
	}
	require.Len(t, snapshot.Tables["users"].Data, 1)
	for _, user := range before.Tables["users"].Data {
		if user["id"] == "owner" {
			require.Equal(t, user, snapshot.Tables["users"].Data[0])
		}
	}
	require.Equal(t, before.Tables["libraries"], snapshot.Tables["libraries"])
	require.Equal(t, before.BodyHashes, snapshot.BodyHashes)
	for _, row := range snapshot.Tables["entries"].Data {
		require.Equal(t, json.Number("0"), row["ingest_api_key_id"])
		require.Equal(t, json.Number("0"), row["last_ingest_api_key_id"])
		require.Empty(t, row["ingest_api_key_name"])
		require.Empty(t, row["last_ingest_api_key_name"])
	}
	policy := snapshot.Tables["chatgpt_policy"].Data[0]
	require.Empty(t, policy["fallback_model_id"])
	require.Equal(t, false, policy["paused"])
	require.Equal(t, json.Number("0"), policy["daily_used"])
	models, err := Models(ctx, clone)
	require.NoError(t, err)
	var subscription map[string]any
	for _, model := range models {
		if model["id"] == "subscription" {
			subscription = model
		}
	}
	require.NotNil(t, subscription)
	require.Empty(t, modelAccountID(subscription))
	require.Equal(t, "needs_configuration", subscription["status"])

	restored := cloneDeliveryTestDatabase(t, admin, clone)
	actual, err := BuildRuntimeManifest(ctx, restored, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(snapshot, actual))
	differences, err := CompareRestoredVectors(ctx, restored, snapshot, actual)
	require.NoError(t, err)
	require.Empty(t, differences)

	// Simulate the effects of a new OAuth login, saving model settings and
	// successful setup-models probes before the second install invocation.
	for _, statement := range []string{
		`INSERT INTO chatgpt_hosts(id,ext_agent_host_id) VALUES(1,'customer-installation')`,
		`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,email,status) VALUES('00000000-0000-0000-0000-000000000002','customer-client','https://auth.openai.com','customer','customer@example.test','connected')`,
		`UPDATE models SET name='customer-catalogue-model', status='active',updated_at=now(),parameters='{"provider":"chatgpt","extra_config":{"chatgpt_account_id":"00000000-0000-0000-0000-000000000002"}}' WHERE id='subscription'`,
		`INSERT INTO models(id,space_id,name,type,source,status,parameters) VALUES('customer-chat',1,'new-catalogue-model','KnowledgeQA','chatgpt','active','{"extra_config":{"chatgpt_account_id":"00000000-0000-0000-0000-000000000002"}}')`,
	} {
		require.NoError(t, restored.Exec(statement).Error)
	}
	policyBefore, err := CaptureChatGPTPolicy(ctx, restored)
	require.NoError(t, err)
	require.NoError(t, restored.Exec(`UPDATE chatgpt_policy SET daily_used=1,day='2026-10-12',updated_at=clock_timestamp()`).Error)
	policyAfter, err := CaptureChatGPTPolicy(ctx, restored)
	require.NoError(t, err)
	require.NoError(t, AppendChatGPTPolicyProbe(ctx, restored, "subscription", policyBefore, policyAfter))
	actual, err = BuildRuntimeManifest(ctx, restored, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifestWithSettings(snapshot, actual, CompareOptions{AllowReconfigurationAppend: true}))
	require.NotEmpty(t, CompareManifest(snapshot, actual))
	require.NoError(t, restored.Exec(`UPDATE libraries SET summary_model_id='customer-chat' WHERE id='library'`).Error)
	actual, err = BuildRuntimeManifest(ctx, restored, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithSettings(snapshot, actual, CompareOptions{AllowReconfigurationAppend: true}), "table libraries")

	after, err := BuildRuntimeManifest(ctx, source, root, "")
	require.NoError(t, err)
	require.Equal(t, before, after)
}
