package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"reliquary/internal/types"
)

// authSession attaches a team identity to both Gin and request contexts.
type authSession struct {
	// User is the authenticated (possibly synthetic) user. Required.
	User      *types.User
	Principal types.Principal
	// TenantID and Tenant identify the personal workspace.
	TenantID uint64
	Tenant   *types.Tenant
}

// applyAuthSession writes the session onto BOTH context surfaces of the
// gin request:
//
//   - c.Keys (read via c.Get / helpers that take *gin.Context)
//   - c.Request.Context() (read via types.*FromContext)
//
// Every auth middleware must go through this helper instead of hand-writing
// the pairs; a key set on only one surface is a latent bug that surfaces as
// "works in handler A, broken in handler B" depending on which read style
// the handler uses.
func applyAuthSession(c *gin.Context, s authSession) {
	ctx := c.Request.Context()
	set := func(key types.ContextKey, v any) {
		c.Set(key.String(), v)
		ctx = context.WithValue(ctx, key, v)
	}

	if s.TenantID != 0 {
		set(types.TenantIDContextKey, s.TenantID)
	}
	if s.Tenant != nil {
		set(types.TenantInfoContextKey, s.Tenant)
	}
	if s.User != nil {
		set(types.UserContextKey, s.User)
		set(types.UserIDContextKey, s.User.ID)
	}
	c.Set(types.PrincipalContextKey.String(), s.Principal)
	ctx = types.WithPrincipal(ctx, s.Principal)

	c.Request = c.Request.WithContext(ctx)
}
