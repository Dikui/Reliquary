package types

import "context"

type libraryReadScopeKey struct{}

// WithLibraryReadScope narrows paginated material reads within the authenticated space.
func WithLibraryReadScope(ctx context.Context, ids []string) context.Context {
	return context.WithValue(ctx, libraryReadScopeKey{}, ids)
}

func LibraryReadScope(ctx context.Context) []string {
	ids, _ := ctx.Value(libraryReadScopeKey{}).([]string)
	return ids
}
