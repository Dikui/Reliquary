package router

import (
	"reliquary/internal/handler"
)

func deploymentCapabilitiesFromRouter(params RouterParams) handler.DeploymentCapabilitiesData {
	return handler.BuildDeploymentCapabilities(handler.Edition, handler.DeploymentFeatureAvailability{
		API: params.TenantHandler != nil && params.TenantAPIKeyService != nil,
	})
}
