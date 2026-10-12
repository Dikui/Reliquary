package types

import (
	"context"
	"strings"
)

const PrincipalWebUser = "web_user"
const PrincipalAPIKey = "api_key"

type Principal struct {
	Type string
	ID   string
	Name string
}

func (p Principal) Normalize() Principal {
	return Principal{
		Type: strings.TrimSpace(p.Type),
		ID:   strings.TrimSpace(p.ID),
		Name: strings.TrimSpace(p.Name),
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
