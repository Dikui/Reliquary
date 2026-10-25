package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"gorm.io/driver/postgres"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
)

func TestEntityMaintenanceCreateAndEdit(t *testing.T) {
	f := newPersonalAuthFixture(t)
	create := personalRequest(t,
		f.router,
		http.MethodPost,
		"/api/v1/entities",
		`{"canonical_name":"Aurora Labs","entity_type":"organization","aliases":["极光实验室"]}`,
		f.key,
		true)
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())
	var result struct{ Data types.Entity }
	require.NoError(t, json.Unmarshal(create.Body.Bytes(), &result))
	require.NotEmpty(t, result.Data.ID)
	require.Equal(t, []string{"极光实验室"}, result.Data.Aliases)
	edit := personalRequest(t,
		f.router,
		http.MethodPatch,
		"/api/v1/entities/"+result.Data.ID,
		`{"canonical_name":"Aurora Research","entity_type":"person","aliases":["Aurora"]}`,
		f.key,
		true)
	require.Equal(t, http.StatusOK, edit.Code, edit.Body.String())
	require.NoError(t, json.Unmarshal(edit.Body.Bytes(), &result))
	require.Equal(t, "Aurora Research", result.Data.CanonicalName)
	require.Equal(t, "person", result.Data.EntityType)
	require.Equal(t, []string{"Aurora"}, result.Data.Aliases)
}

func TestEntityMaintenanceLinksMergeDeleteAndFilter(t *testing.T) {
	f := newPersonalAuthFixture(t)
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		r := personalRequest(t, f.router, method, path, body, f.key, true)
		require.Equal(t, status, r.Code, r.Body.String())
		return r.Body.Bytes()
	}
	create := func(name, kind string) types.Entity {
		t.Helper()
		var result struct{ Data types.Entity }
		require.NoError(t,
			json.Unmarshal(call(http.MethodPost,
				"/api/v1/entities",
				fmt.Sprintf(`{"canonical_name":%q,"entity_type":%q,"aliases":[]}`,
					name,
					kind),
				201),
				&result))
		return result.Data
	}
	keep, merged := create("Retained institution", "organization"), create("Researcher", "person")
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	call(http.MethodPut, "/api/v1/follows/entity/"+merged.ID, "", 200)
	call(http.MethodPut, "/api/v1/follows/entity/"+keep.ID, "", 200)
	var library struct{ Data types.Library }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/library", "", 200), &library))
	entry := types.Entry{
		ID:           uuid.NewString(),
		SpaceID:      types.SingleSpaceID,
		LibraryID:    library.Data.ID,
		Type:         types.EntryTypeManual,
		Title:        "Manually associated material",
		SourceKind:   "article",
		ParseStatus:  types.ParseStatusCompleted,
		EnableStatus: "enabled",
	}
	require.NoError(t, f.db.Create(&entry).Error)
	path := "/api/v1/entries/" + entry.ID + "/entities"
	links := fmt.Sprintf(`{"entity_ids":[%q,%q]}`, keep.ID, merged.ID)
	call(http.MethodPut, path, links, 200)
	var detail struct{ Data types.Entry }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+entry.ID, "", 200), &detail))
	require.True(t, detail.Data.EntitiesManual)
	require.Len(t, detail.Data.Entities, 2)
	// Invalid replacements are atomic and leave the old choice intact.
	for _, body := range []string{
		`{}`,
		`{"entity_ids":null}`,
		`{"entity_ids":["missing"]}`,
		fmt.Sprintf(`{"entity_ids":[%q,%q]}`,
			keep.ID,
			keep.ID),
		`{"entity_ids":["1","2","3","4"]}`,
	} {
		call(http.MethodPut, path, body, 400)
	}
	call(http.MethodPost,
		"/api/v1/entities/merge",
		fmt.Sprintf(`{"keep_entity_id":%q,"merge_entity_id":%q}`,
			keep.ID,
			merged.ID),
		200)
	// A repeated merge is safe, and a chain of merges preserves the old URL.
	call(http.MethodPost,
		"/api/v1/entities/merge",
		fmt.Sprintf(`{"keep_entity_id":%q,"merge_entity_id":%q}`,
			keep.ID,
			merged.ID),
		200)
	var entityDetail struct{ Data types.EntityDetail }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entities/"+merged.ID, "", 200), &entityDetail))
	require.Equal(t, keep.ID, entityDetail.Data.Entity.ID)
	require.Contains(t, entityDetail.Data.Entity.Aliases, "Researcher")
	require.Len(t, entityDetail.Data.Entries, 1)
	var follows struct{ Data []types.FollowTarget }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/follows", "", 200), &follows))
	require.Len(t, follows.Data, 1)
	require.Equal(t, keep.ID, follows.Data[0].TargetID)
	raw := call(http.MethodGet, "/api/v1/entries?entity_ids="+keep.ID, "", 200)
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &envelope))
	// Entry list uses the existing data array contract.
	var listed []types.Entry
	require.NoError(t, json.Unmarshal(envelope["data"], &listed))
	require.Len(t, listed, 1)
	require.Equal(t, entry.ID, listed[0].ID)
	call(http.MethodPut, path, `{"entity_ids":[]}`, 200)
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+entry.ID, "", 200), &detail))
	require.True(t, detail.Data.EntitiesManual)
	require.Empty(t, detail.Data.Entities)
	call(http.MethodDelete, "/api/v1/entities/"+keep.ID, "", 200)
	call(http.MethodGet, "/api/v1/entities/"+merged.ID, "", 404)
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/follows", "", 200), &follows))
	require.Empty(t, follows.Data)
	f.router.GET("/api/v1/operation-logs", handler.NewOperationLogHandler(service.NewOperationLogService(f.db)).List)
	var logs struct{ Data types.OperationLogPage }
	require.NoError(t,
		json.Unmarshal(call(http.MethodGet,
			"/api/v1/operation-logs?operation_type=merge_entities",
			"",
			200),
			&logs))
	require.Len(t, logs.Data.Items, 1)
}

func TestEntityMaintenanceValidationAndPresets(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, body := range []string{
		`{"canonical_name":" ","entity_type":"person"}`,
		`{"canonical_name":"Product","entity_type":"product"}`,
		`{"canonical_name":"Name","entity_type":"other"}`,
		`{"canonical_name":"Name","entity_type":"person","aliases":[""]}`,
	} {
		r := personalRequest(t, f.router, http.MethodPost, "/api/v1/entities", body, f.key, true)
		require.Equal(t, 400, r.Code, r.Body.String())
	}
	// Seed deletion remains effective after a second startup.
	read := func() []types.Entity {
		r := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities?entity=OpenAI", "", f.key, true)
		require.Equal(t, 200, r.Code)
		var result struct{ Data []types.Entity }
		require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
		return result.Data
	}
	seeded := read()
	require.Len(t, seeded, 1)
	r := personalRequest(t, f.router, http.MethodDelete, "/api/v1/entities/"+seeded[0].ID, "", f.key, true)
	require.Equal(t, 200, r.Code)
	require.NoError(t, service.EnsurePersonalSpace(context.Background(), f.db))
	require.Empty(t, read())
}

type retiredExtractionChat struct {
	mcpFixtureChat
	stages []string
}

func (m *retiredExtractionChat) Chat(ctx context.Context,
	messages []chat.Message,
	options *chat.ChatOptions) (*types.ChatResponse,
	error,
) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	m.stages = append(m.stages, stage)
	if stage == "entity_resolution" || stage == "entity_judgment" {
		return nil, fmt.Errorf("retired identity judgment called")
	}
	response, err := m.mcpFixtureChat.Chat(ctx, messages, options)
	if err == nil && stage == "entry_understanding" {
		var result map[string]any
		if err := json.Unmarshal([]byte(response.Content), &result); err != nil {
			return nil, err
		}
		// A legacy or uncooperative model cannot create directory entries.
		result["entities"] = []any{map[string]any{
			"name":         "Unapproved organization",
			"type":         "organization",
			"description":  "Invented profile",
			"relevance":    "central",
			"evidence_ids": []int{1},
		}}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		response.Content = string(raw)
	}
	return response, err
}

func TestEntityMaintenanceProcessingKeepsManualChoiceAndCannotCreateEntities(t *testing.T) {
	model := &retiredExtractionChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	r := ingestAuthHTTPRouter(f)
	RegisterEntityRoutes(r.Group("/api/v1"), handler.NewEntityHandler(service.NewEntityService(f.db, nil)))
	call := func(method, path, body string) []byte {
		t.Helper()
		result := personalRequest(t, r, method, path, body, f.key, true)
		require.Equal(t, 200, result.Code, result.Body.String())
		return result.Body.Bytes()
	}
	readEntities := func() []types.Entity {
		var result struct{ Data []types.Entity }
		require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entities?limit=100", ""), &result))
		return result.Data
	}
	before := readEntities()
	result := ingestRevisionHTTP(t,
		f,
		map[string]any{
			"library":    "personal",
			"source_ref": "manual-entities",
			"title":      "Measurements",
			"content":    "Unapproved organization reported new measurements.",
		})[0]
	id := result["entry_id"].(string)
	process := func() {
		require.NoError(t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
				f.queue.tasks[len(f.queue.tasks)-1]))
	}
	process()
	require.Equal(t, before, readEntities())
	created := personalRequest(t,
		r,
		http.MethodPost,
		"/api/v1/entities",
		`{"canonical_name":"Maintained institution","entity_type":"organization","aliases":[]}`,
		f.key,
		true)
	require.Equal(t, 201, created.Code, created.Body.String())
	var entity struct{ Data types.Entity }
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &entity))
	call(http.MethodPut, "/api/v1/entries/"+id+"/entities", fmt.Sprintf(`{"entity_ids":[%q]}`, entity.Data.ID))
	call(http.MethodPost, "/api/v1/entries/"+id+"/reprocess", `{}`)
	process()
	var entry struct{ Data types.Entry }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+id, ""), &entry))
	require.True(t, entry.Data.EntitiesManual)
	require.Len(t, entry.Data.Entities, 1)
	require.Equal(t, entity.Data.ID, entry.Data.Entities[0].ID)
	revised := ingestRevisionHTTP(t, f, map[string]any{
		"library": "personal", "source_ref": "manual-entities", "title": "Measurements",
		"content": "Corrected original measurements from the maintained institution.",
	})[0]
	require.Equal(t, id, revised["entry_id"])
	process()
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+id, ""), &entry))
	require.True(t, entry.Data.EntitiesManual)
	require.Len(t, entry.Data.Entities, 1)
	require.Equal(t, entity.Data.ID, entry.Data.Entities[0].ID)
	require.NotContains(t, model.stages, "entity_resolution")
	require.NotContains(t, model.stages, "entity_judgment")
}

func TestEntityMaintenanceMigrationRetiresOldDirectoryAndPreservesMaterial(t *testing.T) {
	f := newPersonalAuthFixture(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, f.db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(35))
	var library types.Library
	require.NoError(t, f.db.Where("system_role='official'").First(&library).Error)
	for _, statement := range strings.Split(`
INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,
description,name_embedding,embedding_model_id,embedding_dimension,created_at,
updated_at) VALUES('legacy-entity',1,'Legacy product','legacy product','product',
'Old model profile','[1,0,0]','old',3,now(),now());
 INSERT INTO entity_aliases(id,
space_id,
entity_id,
entity_type,
alias,
normalized_alias,
created_at) VALUES('legacy-alias',
1,
'legacy-entity',
'product',
'Legacy',
'legacy',
now());
 INSERT INTO entries(id,
space_id,
library_id,
type,
title,
source,
source_kind,
parse_status,
vault_body) VALUES('legacy-material',
1,
?,
'manual',
'Retained material',
'',
'article',
'completed',
'Exact original Markdown');
 INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'legacy-material',
'legacy-entity');
 INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity','legacy-entity');`, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if strings.Contains(statement, "?") {
			require.NoError(t, f.db.Exec(statement, library.ID).Error)
		} else {
			require.NoError(t, f.db.Exec(statement).Error)
		}
	}
	require.NoError(t, migration.Migrate(36))
	require.NoError(t, migration.Up())
	require.NoError(t, service.EnsurePersonalSpace(context.Background(), f.db))
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	read := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/legacy-material", "", f.key, true)
	require.Equal(t, 200, read.Code, read.Body.String())
	var entry struct{ Data types.Entry }
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &entry))
	require.Empty(t, entry.Data.Entities)
	body := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/legacy-material/body", "", f.key, true)
	require.Equal(t, 200, body.Code, body.Body.String())
	require.Contains(t, body.Body.String(), "Exact original Markdown")
	retired := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities/legacy-entity", "", f.key, true)
	require.Equal(t, 404, retired.Code)
	follows := personalRequest(t, f.router, http.MethodGet, "/api/v1/follows", "", f.key, true)
	require.Equal(t, 200, follows.Code)
	var targets struct{ Data []types.FollowTarget }
	require.NoError(t, json.Unmarshal(follows.Body.Bytes(), &targets))
	require.Empty(t, targets.Data)
	for _, path := range []string{"/api/v1/entities/merge-suggestions", "/api/v1/entities/name-embeddings/rebuild"} {
		response := personalRequest(t, f.router, http.MethodGet, path, "", f.key, true)
		require.Equal(t, 404, response.Code, response.Body.String())
	}
}
