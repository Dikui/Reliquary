package mcp

import (
	"testing"

	"reliquary/internal/types"
)

func TestApplyAuthHeaders(t *testing.T) {
	tests := []struct {
		name string
		ac   *types.MCPAuthConfig
		want map[string]string
	}{
		{
			name: "nil config injects nothing",
			ac:   nil,
			want: map[string]string{},
		},
		{
			name: "api_key uses default X-API-Key header",
			ac:   &types.MCPAuthConfig{AuthType: types.MCPAuthAPIKey, APIKey: "k1"},
			want: map[string]string{"X-API-Key": "k1"},
		},
		{
			name: "api_key honors custom header name (e.g. raw token in Authorization)",
			ac: &types.MCPAuthConfig{
				AuthType:     types.MCPAuthAPIKey,
				APIKey:       "f7bfde",
				APIKeyHeader: "Authorization",
			},
			want: map[string]string{"Authorization": "f7bfde"},
		},
		{
			name: "bearer adds Bearer prefix",
			ac:   &types.MCPAuthConfig{AuthType: types.MCPAuthBearer, Token: "t1"},
			want: map[string]string{"Authorization": "Bearer t1"},
		},
		{
			name: "selected strategy is exclusive — stale token is not emitted",
			ac: &types.MCPAuthConfig{
				AuthType: types.MCPAuthAPIKey,
				APIKey:   "k1",
				Token:    "stale",
			},
			want: map[string]string{"X-API-Key": "k1"},
		},
		{
			name: "empty AuthType keeps legacy behavior (infer from fields)",
			ac: &types.MCPAuthConfig{
				AuthType: types.MCPAuthNone,
				APIKey:   "k1",
				Token:    "t1",
			},
			want: map[string]string{"X-API-Key": "k1", "Authorization": "Bearer t1"},
		},
		{
			name: "custom headers are always layered on top",
			ac: &types.MCPAuthConfig{
				AuthType:      types.MCPAuthBearer,
				Token:         "t1",
				CustomHeaders: map[string]string{"X-Trace": "abc"},
			},
			want: map[string]string{"Authorization": "Bearer t1", "X-Trace": "abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			applyAuthHeaders(headers, tt.ac)
			if len(headers) != len(tt.want) {
				t.Fatalf("header count = %d, want %d (%v)", len(headers), len(tt.want), headers)
			}
			for k, v := range tt.want {
				if headers[k] != v {
					t.Errorf("header[%q] = %q, want %q", k, headers[k], v)
				}
			}
		})
	}
}
