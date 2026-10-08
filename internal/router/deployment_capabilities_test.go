package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func allDeploymentFeaturesAvailable() handler.DeploymentFeatureAvailability {
	return handler.DeploymentFeatureAvailability{
		Agents: true,
		API:    true,
		MCP:    true,
	}
}

func TestBuildDeploymentCapabilitiesReflectsMissingRoutes(t *testing.T) {
	available := allDeploymentFeaturesAvailable()
	available.MCP = false

	result := handler.BuildDeploymentCapabilities("standard", available)

	for _, key := range []string{"settings.mcp"} {
		capability := result.Capabilities[key]
		if capability.Supported {
			t.Fatalf("%s should be unsupported", key)
		}
		if capability.Reason != "route_not_registered" {
			t.Fatalf("%s reason = %q, want route_not_registered", key, capability.Reason)
		}
	}
	if !result.Capabilities["integrations.api"].Supported {
		t.Fatal("an available route should remain supported")
	}
}

func TestGetDeploymentCapabilitiesHandlerReturnsSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	want := handler.BuildDeploymentCapabilities("standard", allDeploymentFeaturesAvailable())
	systemHandler := &handler.SystemHandler{}
	systemHandler.BindDeploymentCapabilities(want)
	engine.GET("/capabilities", systemHandler.GetDeploymentCapabilities)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body struct {
		Code int                                `json:"code"`
		Data handler.DeploymentCapabilitiesData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || body.Data.Edition != "standard" {
		t.Fatalf("response = %#v", body)
	}
	if !body.Data.Capabilities["settings.mcp"].Supported {
		t.Fatal("MCP capability should be returned")
	}
}
