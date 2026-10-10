package mcp

import (
	"fmt"
	"strings"

	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

// ValidateServiceOutboundURLs validates every URL that the MCP transport
// may contact. It is intentionally called both at
// persistence boundaries and immediately before client construction so stale
// or imported rows cannot bypass the current SSRF policy.
func ValidateServiceOutboundURLs(service *types.MCPService) error {
	if service == nil {
		return fmt.Errorf("MCP service is required")
	}
	if service.URL != nil {
		serviceURL := strings.TrimSpace(*service.URL)
		if serviceURL != "" {
			if err := secutils.ValidateURLForSSRF(serviceURL); err != nil {
				return fmt.Errorf("MCP service URL failed SSRF validation: %w", err)
			}
		}
	}
	return nil
}
