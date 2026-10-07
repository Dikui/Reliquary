package router

import (
	"reliquary/internal/handler"
	"reliquary/internal/sandbox"
)

func deploymentCapabilitiesFromRouter(params RouterParams) handler.DeploymentCapabilitiesData {
	return handler.BuildDeploymentCapabilities(handler.Edition, handler.DeploymentFeatureAvailability{
		Agents: params.CustomAgentHandler != nil,
		IM:     params.IMHandler != nil,
		API:    params.TenantHandler != nil && params.TenantAPIKeyService != nil,
		MCP: params.MCPServiceHandler != nil &&
			params.MCPCredentialsHandler != nil &&
			params.MCPOAuthHandler != nil,
		WebSearch: params.WebSearchHandler != nil &&
			params.WebSearchProviderHandler != nil &&
			params.WebSearchCredentialsHandler != nil,
		VectorStore:   params.VectorStoreHandler != nil,
		Storage:       params.StorageBackendHandler != nil,
		Sandbox:       params.SandboxConfigHandler != nil,
		SandboxDocker: sandbox.DockerBackendEnabled(),
	})
}
