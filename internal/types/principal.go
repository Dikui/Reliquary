package types

import (
	"context"
	"fmt"
	"strings"
)

const (
	PrincipalWebUser         = "web_user"
	PrincipalAPITenant       = "api_tenant"
	PrincipalAPIExternalUser = "api_external_user"
	PrincipalIMUser          = "im_user"
)

// SessionOwnerAPITenantKeyPrefix prefixes sessions.user_id for rows created by a
// tenant API key. The full owner id is "api_tenant_key:<tenantID>:<keyID>", so a
// LIKE '<prefix>%' selects every API-key session in the tenant.
const SessionOwnerAPITenantKeyPrefix = "api_tenant_key:"

// SessionOwnerAPIExternalUserPrefix prefixes sessions.user_id for rows created
// by a tenant API key whose request resolved an external-user identity. The
// remainder is "<tenantID>:<externalUserID>".
const SessionOwnerAPIExternalUserPrefix = PrincipalAPIExternalUser + ":"

// IsAPISessionOwnerID reports whether a stored session owner was produced by
// a tenant API-key request, with or without an external-user identity.
func IsAPISessionOwnerID(ownerID string) bool {
	ownerID = strings.TrimSpace(ownerID)
	return strings.HasPrefix(ownerID, SessionOwnerAPITenantKeyPrefix) ||
		strings.HasPrefix(ownerID, SessionOwnerAPIExternalUserPrefix)
}

// Principal represents the terminal caller for per-subject isolation features.
// Principal storage isolates machine and IM identities from web accounts.
type Principal struct {
	Type string
	ID   string
}

func (p Principal) Normalize() Principal {
	return Principal{
		Type: strings.TrimSpace(p.Type),
		ID:   strings.TrimSpace(p.ID),
	}
}

func (p Principal) Valid() bool {
	p = p.Normalize()
	return p.Type != "" && p.ID != ""
}

func (p Principal) StorageID() string {
	p = p.Normalize()
	if !p.Valid() {
		return ""
	}
	return p.Type + ":" + p.ID
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	principal = principal.Normalize()
	if !principal.Valid() {
		return ctx
	}
	return context.WithValue(ctx, PrincipalContextKey, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	if p, ok := ctx.Value(PrincipalContextKey).(Principal); ok && p.Valid() {
		return p.Normalize(), true
	}
	if uid, ok := UserIDFromContext(ctx); ok && strings.TrimSpace(uid) != "" {
		return Principal{Type: PrincipalWebUser, ID: uid}, true
	}
	return Principal{}, false
}

// MCPOAuthPrincipalFromContext resolves the OAuth token principal for ctx.
func MCPOAuthPrincipalFromContext(ctx context.Context) Principal {
	p, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}
	}
	return p.Normalize()
}

// SessionOwnerIDFromContext scopes sessions to a user or API key principal.
func SessionOwnerIDFromContext(ctx context.Context) string {
	if p, ok := PrincipalFromContext(ctx); ok {
		switch p.Type {
		case PrincipalAPIExternalUser:
			return p.StorageID()
		case PrincipalAPITenant:
			if scope, ok := TenantAPIKeyScopeFromContext(ctx); ok && scope.KeyID > 0 {
				if tenantID, ok := TenantIDFromContext(ctx); ok && tenantID > 0 {
					return fmt.Sprintf("%s%d:%d", SessionOwnerAPITenantKeyPrefix, tenantID, scope.KeyID)
				}
			}
		}
	}
	userID, _ := UserIDFromContext(ctx)
	return userID
}
