package chat

import (
	"context"
	"testing"

	"reliquary/internal/types"
)

func TestUsageAttributionEmptyWithoutAttribution(t *testing.T) {
	// Calls without session or principal context (document parsing, title
	// generation, background jobs) must keep the usage line byte-identical.
	if got := usageAttribution(context.Background()); got != "" {
		t.Fatalf("expected empty suffix, got %q", got)
	}
}

func TestUsageAttributionCarriesPrincipal(t *testing.T) {
	ctx := context.Background()
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "10000:42"})

	got := usageAttribution(ctx)
	want := ", principal=web_user:10000:42"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
