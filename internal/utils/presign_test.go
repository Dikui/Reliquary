package utils

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignFileURL_RoundTrip(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")

	baseURL := "https://reliquary.example.com"
	filePath := "local://1/abc/img.png"
	var spaceID uint64 = 1

	signed, err := SignFileURL(baseURL, filePath, spaceID, 1*time.Hour)
	require.NoError(t, err)
	assert.Contains(t, signed, "https://reliquary.example.com/api/v1/files/presigned")
	assert.Contains(t, signed, "file_path=")
	assert.Contains(t, signed, "space_id=1")
	assert.Contains(t, signed, "sig=")
}

func TestSignFileURL_NoKey(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "")

	_, err := SignFileURL("https://example.com", "local://1/img.png", 1, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SYSTEM_AES_KEY")
}

func TestVerifyFileURLSig_Valid(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")

	filePath := "local://42/knowledge/img.jpg"
	var spaceID uint64 = 42
	key := getPresignKey()
	require.NotNil(t, key)

	expires := time.Now().Add(1 * time.Hour).Unix()
	sig := signPayload(key, filePath, spaceID, expires)

	assert.True(t, VerifyFileURLSig(filePath, spaceID, strconv.FormatInt(expires, 10), sig))
}

func TestVerifyFileURLSig_Expired(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")

	filePath := "local://1/img.png"
	var spaceID uint64 = 1
	key := getPresignKey()
	require.NotNil(t, key)

	expires := time.Now().Add(-1 * time.Hour).Unix() // already expired
	sig := signPayload(key, filePath, spaceID, expires)

	assert.False(t, VerifyFileURLSig(filePath, spaceID, strconv.FormatInt(expires, 10), sig))
}

func TestVerifyFileURLSig_Tampered(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "reliquary-test-aes-key-32bytes!!!")

	filePath := "local://1/img.png"
	var spaceID uint64 = 1
	key := getPresignKey()
	require.NotNil(t, key)

	expires := time.Now().Add(1 * time.Hour).Unix()
	sig := signPayload(key, filePath, spaceID, expires)
	expiresStr := strconv.FormatInt(expires, 10)

	// Tamper with file path
	assert.False(t, VerifyFileURLSig("local://1/other.png", spaceID, expiresStr, sig))
	// Tamper with space ID
	assert.False(t, VerifyFileURLSig(filePath, 999, expiresStr, sig))
	// Tamper with signature
	assert.False(t, VerifyFileURLSig(filePath, spaceID, expiresStr, "deadbeef"))
}

func TestVerifyFileURLSig_NoKey(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "")

	assert.False(t, VerifyFileURLSig("local://1/img.png", 1, "99999999999", "abc"))
}

func TestValidateStoragePathSpace(t *testing.T) {
	assert.NoError(t, ValidateStoragePathSpace("local://42/knowledge/file.pdf", 42))
	assert.Error(t, ValidateStoragePathSpace("local://7/knowledge/file.pdf", 42))
	assert.Error(t, ValidateStoragePathSpace("local://docs/example.txt", 42))
}

func TestValidateLibraryScopedStoragePath(t *testing.T) {
	const spaceID uint64 = 10008

	assert.NoError(t, ValidateLibraryScopedStoragePath("local://10008/exports/img.jpg", spaceID))
	assert.NoError(t, ValidateLibraryScopedStoragePath("minio://bucket/10008/exports/uuid.png", spaceID))
	assert.NoError(t, ValidateLibraryScopedStoragePath("s3://bucket/exports/10008/uuid.png", spaceID))

	// storage://<backendID>/ wrapped paths must resolve to the same space/exports scope.
	assert.NoError(t, ValidateLibraryScopedStoragePath("storage://backend-a/local://10008/exports/img.jpg", spaceID))
	assert.NoError(t, ValidateLibraryScopedStoragePath("storage://backend-a/s3://bucket/ap-test/10008/exports/a.png", spaceID))

	assert.Error(t, ValidateLibraryScopedStoragePath("local://10008/entry-id/123.pdf", spaceID))
	assert.Error(t, ValidateLibraryScopedStoragePath("local://9999/exports/img.jpg", spaceID))
	assert.Error(t, ValidateLibraryScopedStoragePath("local://10008/other/img.jpg", spaceID))
	assert.Error(t, ValidateLibraryScopedStoragePath("storage://backend-a/local://9999/exports/img.jpg", spaceID))
}

func TestParseSpaceIDFromStoragePath(t *testing.T) {
	tests := []struct {
		path string
		want uint64
	}{
		{"local://1/abc/img.png", 1},
		{"local://42/knowledge/file.pdf", 42},
		{"minio://bucket/1/abc/img.png", 1},
		{"s3://bucket/reliquary/1/abc/img.png", 1},
		{"s3://bucket/region/prefix/1/abc/img.png", 1},
		{"s3://bucket/1/abc/img.png", 1},
		{"s3://bucket/reliquary/1/abc/img.png", 1},
		{"https://example.com/img.png", 0},
		{"invalid", 0},
		{"local://exports/file.csv", 0},
		{"storage://backend-a/local://1/abc/img.png", 1},
		{"storage://backend-a/s3://bucket/region/prefix/42/abc/img.png", 42},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ParseSpaceIDFromStoragePath(tt.path)
			assert.Equal(t, tt.want, got, "ParseSpaceIDFromStoragePath(%q)", tt.path)
		})
	}
}
