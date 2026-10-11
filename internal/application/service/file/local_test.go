package file

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractSpaceIDFromPresignedURL pulls the space_id query parameter from a
// signed URL. Returns "" when the URL is not parseable as a presigned URL.
func extractSpaceIDFromPresignedURL(t *testing.T, presigned string) string {
	t.Helper()
	u, err := url.Parse(presigned)
	require.NoError(t, err)
	return u.Query().Get("space_id")
}

// TestLocalGetFileURL_SpaceIDFromPath verifies that space ID is extracted
// from the storage path — which encodes the resource owner, so cross-space
// shared resources resolve to the correct owning space's storage config.
func TestLocalGetFileURL_SpaceIDFromPath(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")

	svc := NewLocalFileService("/data/files", "https://reliquary.example.com")

	got, err := svc.GetFileURL(context.Background(), "local://7/abc/img.png")
	require.NoError(t, err)
	assert.Equal(t, "7", extractSpaceIDFromPresignedURL(t, got))
}

// TestLocalGetFileURL_NoExternalURL verifies backward compatibility: without
// APP_EXTERNAL_URL, GetFileURL still returns the local:// path unchanged.
func TestLocalGetFileURL_NoExternalURL(t *testing.T) {
	svc := NewLocalFileService("/data/files", "")

	got, err := svc.GetFileURL(context.Background(), "local://1/abc/img.png")
	require.NoError(t, err)
	assert.Equal(t, "local://1/abc/img.png", got)
}
