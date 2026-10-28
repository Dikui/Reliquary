package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The repository ships no domains; tests that need choices seed two.
func seedTestDomains(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	for _, name := range []string{"Alpha", "Beta"} {
		require.NoError(t, db.Create(&types.Domain{
			ID: "domain-" + name, SpaceID: types.SingleSpaceID, Name: name, NormalizedName: types.NormalizeEntityKey(name),
			Definition: name + " fixture direction.", CreatedAt: now, UpdatedAt: now,
		}).Error)
	}
}

type domainResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

func TestDomainMaintenanceMaterialChoicesAndMixedFilters(t *testing.T) {
	f := newMCPFixture(t)
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		r := personalRequest(t, f.router, method, path, body, f.key, true)
		require.Equal(t, status, r.Code, r.Body.String())
		return r.Body.Bytes()
	}
	seedTestDomains(t, f.db)
	var domains struct{ Data []domainResponse }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/domains", "", 200), &domains))
	agent, llm := domains.Data[0], domains.Data[1]
	var entity struct{ Data types.Entity }
	require.NoError(
		t,
		json.Unmarshal(
			call(
				http.MethodPost,
				"/api/v1/entities",
				`{"canonical_name":"Filter institution", "aliases":[]}`,
				201,
			),
			&entity,
		),
	)
	a := f.seedArticle(t, "domainfilterfixture Mixed match", "", "domainfilterfixture shared material")
	b := f.seedArticle(t, "domainfilterfixture Entity only", "", "domainfilterfixture other material")
	c := f.seedArticle(t, "domainfilterfixture Domain only", "", "domainfilterfixture third material")
	for _, id := range []string{a.ID, b.ID} {
		call(http.MethodPut, "/api/v1/entries/"+id+"/entities", fmt.Sprintf(`{"entity_ids":[%q]}`, entity.Data.ID), 200)
	}
	path := "/api/v1/entries/" + a.ID + "/domains"
	call(http.MethodPut, path, fmt.Sprintf(`{"domain_ids":[%q,%q]}`, agent.ID, llm.ID), 200)
	call(http.MethodPut, "/api/v1/entries/"+c.ID+"/domains", fmt.Sprintf(`{"domain_ids":[%q]}`, agent.ID), 200)
	for _, body := range []string{
		`{}`, `{"domain_ids":null}`, `{"domain_ids":["missing"]}`,
		`{"domain_ids":["1","2","3","4"]}`,
		fmt.Sprintf(`{"domain_ids":[%q,%q]}`, agent.ID, agent.ID),
	} {
		call(http.MethodPut, path, body, 400)
	}
	read := func(id string) map[string]json.RawMessage {
		t.Helper()
		var response struct{ Data map[string]json.RawMessage }
		require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+id, "", 200), &response))
		return response.Data
	}
	var selected []domainResponse
	detail := read(a.ID)
	require.JSONEq(t, `true`, string(detail["domains_manual"]))
	require.NoError(t, json.Unmarshal(detail["domains"], &selected))
	require.Len(t, selected, 2)
	var listed struct{ Data []types.Entry }
	require.NoError(
		t,
		json.Unmarshal(
			call(http.MethodGet, "/api/v1/entries?domain_ids="+agent.ID+"&entity_ids="+entity.Data.ID, "", 200),
			&listed,
		),
	)
	require.Len(t, listed.Data, 1)
	require.Equal(t, a.ID, listed.Data[0].ID)
	client := f.connect(t, "2025-06-18")
	var available struct{ Domains []domainResponse }
	mcpCall(t, client, "list_domains", map[string]any{}, &available)
	require.Len(t, available.Domains, 2)
	var results types.EntrySearchResponse
	mcpCall(
		t,
		client,
		"search",
		map[string]any{
			"libraries":  []string{f.library.ID},
			"query":      "domainfilterfixture",
			"domain_ids": []string{agent.ID},
			"entity_ids": []string{entity.Data.ID},
		},
		&results,
	)
	require.Len(t, results.Results, 1)
	require.Equal(t, a.ID, results.Results[0].EntryID)
	call(
		http.MethodPatch,
		"/api/v1/domains/"+agent.ID,
		`{"name":"Agent systems","definition":"Autonomous planning and tool use."}`,
		200,
	)
	require.NoError(t, json.Unmarshal(read(a.ID)["domains"], &selected))
	require.Equal(t, "Agent systems", selected[0].Name)
	call(http.MethodDelete, "/api/v1/domains/"+agent.ID, "", 200)
	require.NoError(t, json.Unmarshal(read(a.ID)["domains"], &selected))
	require.Len(t, selected, 1)
	require.Equal(t, llm.ID, selected[0].ID)
	call(http.MethodPut, path, `{"domain_ids":[]}`, 200)
	detail = read(a.ID)
	require.JSONEq(t, `true`, string(detail["domains_manual"]))
	require.JSONEq(t, `[]`, string(detail["domains"]))
	require.JSONEq(t, `true`, string(read(c.ID)["domains_manual"]))
	call(http.MethodPut, "/api/v1/entries/missing/domains", `{"domain_ids":[]}`, 404)
}

func TestDomainMaintenanceCRUD(t *testing.T) {
	f := newPersonalAuthFixture(t)
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		r := personalRequest(t, f.router, method, path, body, f.key, true)
		require.Equal(t, status, r.Code, r.Body.String())
		return r.Body.Bytes()
	}
	list := func() []domainResponse {
		t.Helper()
		var response struct{ Data []domainResponse }
		require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/domains", "", 200), &response))
		return response.Data
	}
	// The repository ships no domain presets; the catalogue starts empty.
	require.Empty(t, list())
	for _, body := range []string{`{}`, `{"name":" ","definition":"x"}`, `{"name":"Robotics","definition":" "}`} {
		call(http.MethodPost, "/api/v1/domains", body, 400)
	}
	var created struct{ Data domainResponse }
	require.NoError(
		t,
		json.Unmarshal(
			call(
				http.MethodPost,
				"/api/v1/domains",
				`{"name":"Robotics","definition":"Robots and embodied control."}`,
				201,
			),
			&created,
		),
	)
	require.NotEmpty(t, created.Data.ID)
	call(http.MethodPost, "/api/v1/domains", `{"name":"Robotics","definition":"Duplicate"}`, 409)
	call(
		http.MethodPatch,
		"/api/v1/domains/"+created.Data.ID,
		`{"name":"Embodied AI","definition":"Learning to act in the physical world."}`,
		200,
	)
	items := list()
	require.Len(t, items, 1)
	require.Equal(t, "Learning to act in the physical world.", items[0].Definition)
	call(http.MethodDelete, "/api/v1/domains/"+created.Data.ID, "", 200)
	require.NoError(t, service.EnsurePersonalSpace(context.Background(), f.db))
	require.Empty(t, list())
	call(http.MethodPatch, "/api/v1/domains/missing", `{"name":"Unknown","definition":"Unknown direction."}`, 404)
	call(http.MethodDelete, "/api/v1/domains/missing", "", 404)
}

func TestDomainMaintenanceManualChoiceSurvivesProcessingAndRevision(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	r := ingestAuthHTTPRouter(f)
	RegisterDomainRoutes(r.Group("/api/v1"), handler.NewDomainHandler(service.NewDomainService(f.db)))
	call := func(method, path, body string) []byte {
		t.Helper()
		response := personalRequest(t, r, method, path, body, f.key, true)
		require.Equal(t, 200, response.Code, response.Body.String())
		return response.Body.Bytes()
	}
	seedTestDomains(t, f.db)
	var domains struct{ Data []domainResponse }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/domains", ""), &domains))
	result := ingestRevisionHTTP(t, f, map[string]any{
		"library": "personal", "source_ref": "manual-domains", "title": "Measurements",
		"content": "Initial original measurements.",
	})[0]
	id := result["entry_id"].(string)
	read := func() types.Entry {
		t.Helper()
		var result struct{ Data types.Entry }
		require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+id, ""), &result))
		return result.Data
	}
	process := func() {
		t.Helper()
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
	}
	call(http.MethodPut, "/api/v1/entries/"+id+"/domains", fmt.Sprintf(`{"domain_ids":[%q]}`, domains.Data[0].ID))
	process()
	entry := read()
	require.True(t, entry.DomainsManual)
	require.Len(t, entry.Domains, 1)
	call(http.MethodPost, "/api/v1/entries/"+id+"/reprocess", `{}`)
	process()
	require.Len(t, read().Domains, 1)
	revised := ingestRevisionHTTP(t, f, map[string]any{
		"library": "personal", "source_ref": "manual-domains", "title": "Measurements",
		"content": "Corrected original measurements.",
	})[0]
	require.Equal(t, id, revised["entry_id"])
	process()
	entry = read()
	require.True(t, entry.DomainsManual)
	require.Len(t, entry.Domains, 1)
	call(http.MethodPut, "/api/v1/entries/"+id+"/domains", `{"domain_ids":[]}`)
	call(http.MethodPost, "/api/v1/entries/"+id+"/reprocess", `{}`)
	process()
	entry = read()
	require.True(t, entry.DomainsManual)
	require.Empty(t, entry.Domains)
}
