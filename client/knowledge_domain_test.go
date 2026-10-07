package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKnowledgeDomainCRUDWireContract(t *testing.T) {
	var calls []string
	domain := KnowledgeDomain{ID: "domain-ai", Name: "AI 行业", Fields: []DomainField{{Key: "capacity", ValueType: "number", Aliases: []string{"容量"}, UnitFamily: "tokens"}}, UnitFamilies: []DomainUnitFamily{{Key: "tokens", CanonicalUnit: "token", Units: []DomainUnit{{Name: "k tokens", Factor: 1000}}}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body KnowledgeDomain
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Fields[0].UnitFamily != "tokens" || body.UnitFamilies[0].Units[0].Factor != 1000 {
				t.Errorf("template not preserved: %+v", body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		var data any = domain
		if r.URL.Path == "/api/v1/knowledge-domains" && r.Method == http.MethodGet {
			data = []KnowledgeDomain{domain}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, WithAPIKey("sk-test"))
	ctx := context.Background()
	list, err := c.ListKnowledgeDomains(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	got, err := c.GetKnowledgeDomain(ctx, domain.ID)
	if err != nil || got.Name != domain.Name {
		t.Fatalf("get: %v %v", got, err)
	}
	if _, err := c.CreateKnowledgeDomain(ctx, &domain); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateKnowledgeDomain(ctx, domain.ID, &domain); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteKnowledgeDomain(ctx, domain.ID); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/v1/knowledge-domains", "GET /api/v1/knowledge-domains/domain-ai", "POST /api/v1/knowledge-domains", "PUT /api/v1/knowledge-domains/domain-ai", "DELETE /api/v1/knowledge-domains/domain-ai"}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d: %s", i, calls[i])
		}
	}
}
