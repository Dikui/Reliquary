package docs_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	docs "reliquary/docs"
)

type swaggerDocumentCase struct {
	name     string
	loadSpec func(t *testing.T) []byte
	parse    func([]byte, any) error
}

func swaggerDocuments() []swaggerDocumentCase {
	return []swaggerDocumentCase{
		{
			name: "registered document",
			loadSpec: func(t *testing.T) []byte {
				t.Helper()
				return []byte(docs.SwaggerInfo.ReadDoc())
			},
			parse: json.Unmarshal,
		},
		{
			name: "swagger.json",
			loadSpec: func(t *testing.T) []byte {
				t.Helper()
				return readSwaggerFile(t, "swagger.json")
			},
			parse: json.Unmarshal,
		},
		{
			name: "swagger.yaml",
			loadSpec: func(t *testing.T) []byte {
				t.Helper()
				return readSwaggerFile(t, "swagger.yaml")
			},
			parse: yaml.Unmarshal,
		},
	}
}

func TestKnowledgeSearchRouteContract(t *testing.T) {
	for _, tt := range swaggerDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			assertKnowledgeSearchRouteContract(t, tt.loadSpec(t), tt.parse)
		})
	}
}

func TestModelDeleteUsageContract(t *testing.T) {
	for _, tt := range swaggerDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			assertModelDeleteUsageContract(t, tt.loadSpec(t), tt.parse)
		})
	}
}

func readSwaggerFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func assertKnowledgeSearchRouteContract(t *testing.T, data []byte, parse func([]byte, any) error) {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `json:"paths" yaml:"paths"`
	}
	if err := parse(data, &spec); err != nil {
		t.Fatalf("parse generated Swagger document: %v", err)
	}

	knowledgeSearch, ok := spec.Paths["/knowledge-search"]
	if !ok {
		t.Fatal("generated Swagger document does not expose /knowledge-search")
	}
	if _, ok := knowledgeSearch["post"]; !ok {
		t.Fatal("generated Swagger document does not expose POST /knowledge-search")
	}

	if staleRoute, ok := spec.Paths["/sessions/search"]; ok {
		if _, ok := staleRoute["post"]; ok {
			t.Fatal("generated Swagger document still exposes stale POST /sessions/search")
		}
	}
}

func assertModelDeleteUsageContract(t *testing.T, data []byte, parse func([]byte, any) error) {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]struct {
			Responses map[string]any `json:"responses" yaml:"responses"`
		} `json:"paths" yaml:"paths"`
		Definitions map[string]map[string]any `json:"definitions" yaml:"definitions"`
	}
	if err := parse(data, &spec); err != nil {
		t.Fatalf("parse generated Swagger document: %v", err)
	}

	models, ok := spec.Paths["/models/{id}"]
	if !ok {
		t.Fatal("generated Swagger document does not expose /models/{id}")
	}
	deleteOperation, ok := models["delete"]
	if !ok {
		t.Fatal("generated Swagger document does not expose DELETE /models/{id}")
	}
	if _, ok := deleteOperation.Responses["400"]; !ok {
		t.Fatal("model DELETE Swagger contract does not document the model-in-use 400 response")
	}

	const errorCodeDefinition = "reliquary_internal_errors.ErrorCode"
	errorCodes, ok := spec.Definitions[errorCodeDefinition]
	if !ok {
		t.Fatalf("generated Swagger document does not expose %s", errorCodeDefinition)
	}
	enum, ok := errorCodes["enum"].([]any)
	if !ok {
		t.Fatalf("generated Swagger ErrorCode enum has unexpected shape: %#v", errorCodes["enum"])
	}
	enumVarNames, ok := errorCodes["x-enum-varnames"].([]any)
	if !ok {
		t.Fatalf("generated Swagger ErrorCode names have unexpected shape: %#v", errorCodes["x-enum-varnames"])
	}
	for i, value := range enum {
		if swaggerInteger(value) == 2300 {
			if i >= len(enumVarNames) || enumVarNames[i] != "ErrModelInUse" {
				t.Fatalf("error code 2300 must align with ErrModelInUse, got names=%v", enumVarNames)
			}
			return
		}
	}
	t.Fatal("generated Swagger ErrorCode enum does not include model-in-use code 2300")
}

func TestPersonalSpaceRouteContract(t *testing.T) {
	for _, tt := range swaggerDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			var spec struct {
				Paths       map[string]map[string]any `json:"paths" yaml:"paths"`
				Definitions map[string]struct {
					Properties map[string]any `json:"properties" yaml:"properties"`
				} `json:"definitions" yaml:"definitions"`
			}
			if err := tt.parse(tt.loadSpec(t), &spec); err != nil {
				t.Fatal(err)
			}
			for path, methods := range map[string][]string{
				"/space":                   {"get", "put"},
				"/space/api-keys":          {"get", "post"},
				"/space/api-keys/{key_id}": {"put", "delete"},
				"/space/kv/{key}":          {"get", "put"},
				"/space/model-tiers":       {"get", "put"},
				"/system/settings":         {"get"},
				"/system/settings/{key}":   {"get", "put", "delete"},
				"/system/runtime/queues":   {"get"},
			} {
				for _, method := range methods {
					if _, ok := spec.Paths[path][method]; !ok {
						t.Errorf("missing %s %s", method, path)
					}
				}
			}
			for path := range spec.Paths {
				if strings.HasPrefix(path, "/tenants/") || strings.HasPrefix(path, "/system/admin/") {
					t.Errorf("stale route %s", path)
				}
			}
			for name, definition := range spec.Definitions {
				if _, ok := definition.Properties["storage_quota"]; ok {
					t.Errorf("stale storage quota in %s", name)
				}
			}
			info := spec.Definitions["internal_handler.GetSystemInfoResponse"]
			if _, ok := info.Properties["credential_encryption_enabled"]; !ok {
				t.Error("system info does not document credential encryption state")
			}
		})
	}
}

func swaggerInteger(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case float64:
		return int(number)
	default:
		return 0
	}
}

func TestFocusPrecheckAndBackfillContract(t *testing.T) {
	for _, tt := range swaggerDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			var spec struct {
				Paths       map[string]map[string]any `json:"paths" yaml:"paths"`
				Definitions map[string]struct {
					Properties map[string]any `json:"properties" yaml:"properties"`
				} `json:"definitions" yaml:"definitions"`
			}
			if err := tt.parse(tt.loadSpec(t), &spec); err != nil {
				t.Fatal(err)
			}
			for path, method := range map[string]string{
				"/knowledge-context/backfill/estimate":           "get",
				"/knowledge-context/backfill":                    "post",
				"/knowledge-context/backfill/batches":            "get",
				"/knowledge-context/backfill/batches/{batch_id}": "get",
			} {
				if _, ok := spec.Paths[path][method]; !ok {
					t.Errorf("missing %s %s", method, path)
				}
			}
			for definition, fields := range map[string][]string{
				"reliquary_internal_types.KnowledgeEvidence":                {"extraction_path", "backfilled_at", "backfill_template_updated_at"},
				"reliquary_internal_types.KnowledgeContextResult":           {"template_only_message_count", "events_coverage_notice"},
				"reliquary_internal_types.KnowledgeContextBackfillEstimate": {"pending_message_count", "batch_message_count", "remaining_message_count", "estimated_model_calls", "template_only_message_count"},
				"reliquary_internal_types.KnowledgeContextBackfillBatch":    {"entity_id", "topic_id", "message_count", "success_count", "failure_count", "status"},
			} {
				for _, field := range fields {
					if _, ok := spec.Definitions[definition].Properties[field]; !ok {
						t.Errorf("missing %s.%s", definition, field)
					}
				}
			}
		})
	}
}

func TestWorkflowDisabledConflictContract(t *testing.T) {
	for _, tt := range swaggerDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			var spec struct {
				Paths map[string]map[string]struct {
					Responses map[string]struct {
						Description string `json:"description" yaml:"description"`
					} `json:"responses" yaml:"responses"`
				} `json:"paths" yaml:"paths"`
			}
			if err := tt.parse(tt.loadSpec(t), &spec); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/knowledge-bases/{id}/workflows/{workflow_id}/run", "/knowledge-bases/{id}/workflows/runs/{run_id}/retry", "/knowledge-bases/{id}/workflows/runs/{run_id}/resume"} {
				response, ok := spec.Paths[path]["post"].Responses["409"]
				if !ok || !strings.Contains(response.Description, "Workflow disabled") {
					t.Errorf("%s must document disabled workflow conflict: %+v", path, response)
				}
			}
		})
	}
}
