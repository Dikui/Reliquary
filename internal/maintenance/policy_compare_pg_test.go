package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
)

func copyManifest(t *testing.T, manifest *Manifest) *Manifest {
	t.Helper()
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	var result Manifest
	require.NoError(t, json.Unmarshal(data, &result))
	return &result
}

func TestParserCredentialRefillAllowsOnlyItsSpacesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	root := t.TempDir()
	expected, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	_, err = Scrub(ctx, db)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE spaces SET parser_engine_config='{"nested":{"mineru_api_key":"REFILLED_PARSER_SECRET","endpoint":"http://localhost"}}',updated_at=clock_timestamp() WHERE id=1`).Error)
	actual, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, actual))
	withoutParserRequirement := copyManifest(t, expected)
	for i := range withoutParserRequirement.Required {
		if withoutParserRequirement.Required[i].Kind == "spaces" {
			withoutParserRequirement.Required[i].Fields = []string{"credentials"}
		}
	}
	require.Contains(t, CompareManifest(withoutParserRequirement, actual), "table spaces")
	wrongSpaceRequirement := copyManifest(t, expected)
	for i := range wrongSpaceRequirement.Required {
		if wrongSpaceRequirement.Required[i].Kind == "spaces" {
			wrongSpaceRequirement.Required[i].ID = "2"
		}
	}
	require.Contains(t, CompareManifest(wrongSpaceRequirement, actual), "table spaces", "a reminder for another space cannot permit this timestamp")
	// Re-entering a credential cannot allow a public parser endpoint or any
	// other space metadata to change, even on the permitted space row.
	for _, mutation := range []struct{ Change, Undo string }{
		{`UPDATE spaces SET parser_engine_config=jsonb_set(parser_engine_config,'{nested,endpoint}','"http://changed-public-parser"') WHERE id=1`, `UPDATE spaces SET parser_engine_config=jsonb_set(parser_engine_config,'{nested,endpoint}','"http://localhost"') WHERE id=1`},
		{`UPDATE spaces SET name='Changed owner' WHERE id=1`, `UPDATE spaces SET name='Owner' WHERE id=1`},
	} {
		require.NoError(t, db.Exec(mutation.Change).Error)
		changed, err := BuildManifest(ctx, db, root, "")
		require.NoError(t, err)
		require.Contains(t, CompareManifest(expected, changed), "table spaces")
		if mutation.Undo != "" {
			require.NoError(t, db.Exec(mutation.Undo).Error)
		}
	}
}

func TestPortableParserRemindersSurviveSanitizedFlushAndVaultOnlyRebuild(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	root := t.TempDir()
	parserFields := func(required []Requirement) []string {
		var fields []string
		for _, requirement := range required {
			if requirement.Kind == "spaces" && requirement.ID == "1" {
				for _, field := range requirement.Fields {
					if parserCredentialRequirement(field) {
						fields = append(fields, field)
					}
				}
			}
		}
		return fields
	}
	before, err := Capture(ctx, source)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"parser_engine_config.nested.mineru_api_key"}, parserFields(before.Required))
	require.NoError(t, WriteSnapshot(root, "", before))
	_, err = Scrub(ctx, source)
	require.NoError(t, err)
	after, err := Capture(ctx, source)
	require.NoError(t, err)
	require.Empty(t, parserFields(after.Required), "the actual inventory must not pretend an omitted parser key has been supplied")
	require.NoError(t, WriteSnapshot(root, "", after))
	portable, err := Load(root)
	require.NoError(t, err)
	require.Equal(t, parserFields(before.Required), parserFields(portable.Required))
	target := pgtest.Open(t)
	imported, err := Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	require.Equal(t, parserFields(before.Required), parserFields(imported.Required))
	current, err := Capture(ctx, target)
	require.NoError(t, err)
	require.Empty(t, parserFields(current.Required))
	require.NoError(t, WriteSnapshot(root, "", current))
	portable, err = Load(root)
	require.NoError(t, err)
	require.Equal(t, parserFields(before.Required), parserFields(portable.Required))
	manifest, err := BuildManifest(ctx, target, root, "")
	require.NoError(t, err)
	require.Empty(t, parserFields(manifest.Required), "ready checks consume actual field presence, not the portable reminder")
	path := filepath.Join(root, ".reliquary", "manifest.json")
	require.NoError(t, os.WriteFile(path, []byte("unreadable old reminder"), 0600))
	repaired, err := VerifyReplica(ctx, target, root, "", true)
	require.NoError(t, err)
	require.True(t, repaired.OK)
	portable, err = Load(root)
	require.NoError(t, err)
	require.Equal(t, parserFields(before.Required), parserFields(portable.Required), "the valid redundant copy preserves omitted original parser paths")
	require.NoError(t, target.Exec(`UPDATE spaces SET parser_engine_config=jsonb_set(parser_engine_config,'{nested,mineru_api_key}',to_jsonb(?::text)),updated_at=clock_timestamp() WHERE id=1`, "new-parser-secret-on-target").Error)
	filled, err := Capture(ctx, target)
	require.NoError(t, err)
	require.Equal(t, parserFields(before.Required), parserFields(filled.Required))
	require.NoError(t, WriteSnapshot(root, "", filled))
	// An unreadable sole copy of provenance cannot be silently overwritten.
	require.NoError(t, os.WriteFile(path, []byte("unreadable old reminder"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".reliquary", "config", "required_credentials.json"), []byte("unreadable redundant reminder"), 0600))
	require.ErrorContains(t, WriteSnapshot(root, "", filled), "restore them from backup")
	bytes, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "unreadable old reminder", string(bytes))
	// A pre-redundancy replica can still be repaired from an original database
	// whose parser secret remains configured and exposes its field inventory.
	legacyRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(legacyRoot, "", filled))
	require.NoError(t, os.Remove(filepath.Join(legacyRoot, ".reliquary", "config", "required_credentials.json")))
	require.NoError(t, os.WriteFile(filepath.Join(legacyRoot, ".reliquary", "manifest.json"), []byte("corrupt pre-redundancy manifest"), 0600))
	repaired, err = VerifyReplica(ctx, target, legacyRoot, "", true)
	require.NoError(t, err)
	require.True(t, repaired.OK)
	portable, err = Load(legacyRoot)
	require.NoError(t, err)
	require.Equal(t, parserFields(filled.Required), parserFields(portable.Required))
}

func seedProbeModel(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO models(id,space_id,name,type,source,parameters) VALUES('chatgpt-probe',1,'chatgpt-probe','KnowledgeQA','chatgpt','{}')`).Error)
}

func appendPolicyTransition(t *testing.T, db *gorm.DB, statement string) {
	t.Helper()
	ctx := context.Background()
	before, err := CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.NoError(t, db.Exec(statement).Error)
	after, err := CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.NoError(t, AppendChatGPTPolicyProbe(ctx, db, "chatgpt-probe", before, after))
}

func TestReconfigurationPolicyProbeRequiresCompleteImmutableAuditChain(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	seedProbeModel(t, db)
	require.NoError(t, db.Exec(`INSERT INTO chatgpt_policy(id,daily_limit,daily_used,day,paused,reason,retry_at,fallback_model_id,updated_at) VALUES(1,10,2,'2026-10-11',false,'',NULL,'chat','2026-10-11T01:00:00Z')`).Error)
	root := t.TempDir()
	expected, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	appendPolicyTransition(t, db, `UPDATE chatgpt_policy SET daily_used=3,updated_at=clock_timestamp() WHERE id=1`)
	appendPolicyTransition(t, db, `UPDATE chatgpt_policy SET daily_used=4,paused=true,reason='rate_limit',retry_at='2026-10-12T01:00:00Z',updated_at=clock_timestamp() WHERE id=1`)
	actual, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table chatgpt_policy")
	require.Contains(t, CompareManifestWithOptions(expected, actual, false, true), "table chatgpt_policy")
	options := CompareOptions{AllowReconfigurationAppend: true}
	require.Empty(t, CompareManifestWithSettings(copyManifest(t, expected), copyManifest(t, actual), options))
	// The actual quota remains consumed. Comparison never modifies policy.
	var used int
	require.NoError(t, db.Table("chatgpt_policy").Select("daily_used").Scan(&used).Error)
	require.Equal(t, 4, used)
	probeRows := func(manifest *Manifest) []map[string]any {
		var rows []map[string]any
		for _, row := range manifest.Tables["operation_logs"].Data {
			if row["operation_type"] == "maintenance_model_probe" {
				rows = append(rows, row)
			}
		}
		return rows
	}
	for _, test := range []struct {
		Name   string
		Mutate func(*Manifest)
	}{
		{"unaudited policy mutation", func(current *Manifest) { current.Tables["chatgpt_policy"].Data[0]["daily_used"] = 5 }},
		{"missing transition", func(current *Manifest) {
			entry := current.Tables["operation_logs"]
			rows := []map[string]any{}
			dropped := false
			for _, row := range entry.Data {
				if row["operation_type"] == "maintenance_model_probe" && !dropped {
					dropped = true
					continue
				}
				rows = append(rows, row)
			}
			entry.Data, entry.Rows = rows, int64(len(rows))
			current.Tables["operation_logs"] = entry
		}},
		{"discontinuous before", func(current *Manifest) {
			probeRows(current)[1]["before_value"].([]any)[0].(map[string]any)["daily_used"] = 99
		}},
		{"partial row audit", func(current *Manifest) {
			delete(probeRows(current)[0]["after_value"].([]any)[0].(map[string]any), "reason")
		}},
		{"wrong actor", func(current *Manifest) { probeRows(current)[0]["actor"] = "owner" }},
		{"wrong model", func(current *Manifest) { probeRows(current)[0]["object_id"] = "chat" }},
		{"ambiguous chronological order", func(current *Manifest) { rows := probeRows(current); rows[1]["created_at"] = rows[0]["created_at"] }},
		{"configuration changed with matching audit", func(current *Manifest) {
			current.Tables["chatgpt_policy"].Data[0]["daily_limit"] = 99
			for _, row := range probeRows(current) {
				row["after_value"].([]any)[0].(map[string]any)["daily_limit"] = 99
			}
		}},
	} {
		t.Run(test.Name, func(t *testing.T) {
			changed := copyManifest(t, actual)
			test.Mutate(changed)
			require.Contains(t, CompareManifestWithSettings(expected, changed, options), "chatgpt policy probe audit chain")
		})
	}
	oldLogTampered := copyManifest(t, actual)
	for _, row := range oldLogTampered.Tables["operation_logs"].Data {
		if row["id"] == "op-1" {
			row["actor"] = "changed-history"
		}
	}
	require.Contains(t, CompareManifestWithSettings(expected, oldLogTampered, options), "table operation_logs")
}

func TestFirstAuditedPolicyReservationPreservesAbsentOriginalAndDefaultConfiguration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	seedProbeModel(t, db)
	root := t.TempDir()
	expected, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Zero(t, expected.Tables["chatgpt_policy"].Rows)
	appendPolicyTransition(t, db, `INSERT INTO chatgpt_policy(id,daily_limit,daily_used,day,paused,reason,retry_at,fallback_model_id) VALUES(1,NULL,1,'2026-10-11',false,'',NULL,'')`)
	actual, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifestWithSettings(expected, actual, CompareOptions{AllowReconfigurationAppend: true}))
	require.Contains(t, strings.Join(CompareManifest(expected, actual), ","), "table chatgpt_policy")
	appendPolicyTransition(t, db, `UPDATE chatgpt_policy SET fallback_model_id='chat',updated_at=clock_timestamp() WHERE id=1`)
	changed, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifestWithSettings(expected, changed, CompareOptions{AllowReconfigurationAppend: true}), "chatgpt policy probe audit chain")
}
