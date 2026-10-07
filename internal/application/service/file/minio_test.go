package file

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestMinioSharedS3StorageRoundTrip(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	objects := make(map[string][]byte)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := strings.TrimPrefix(r.URL.Path, "/")
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			if source := r.Header.Get("X-Amz-Copy-Source"); source != "" {
				objects[key] = append([]byte(nil), objects[strings.TrimPrefix(source, "/")]...)
				w.Header().Set("Content-Type", "application/xml")
				io.WriteString(w, `<CopyObjectResult><ETag>"copied"</ETag></CopyObjectResult>`)
				return
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[key] = data
			w.Header().Set("ETag", `"saved"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, exists := objects[key]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(data)
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	svc, provider, err := NewFileServiceFromStorageConfig("minio", &types.StorageEngineConfig{
		MinIO: &types.MinIOEngineConfig{Mode: "remote", Endpoint: strings.TrimPrefix(server.URL, "http://"), AccessKeyID: "ak", SecretAccessKey: "sk", BucketName: "bucket", PathPrefix: "prefix", UseSSL: false},
	}, "")
	require.NoError(t, err)
	require.Equal(t, "minio", provider)
	require.IsType(t, &s3FileService{}, svc)
	require.True(t, svc.(*s3FileService).client.Options().UsePathStyle)
	ctx := context.Background()
	path, err := svc.SaveBytes(ctx, []byte("saved content"), 42, "source.txt", false)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(path, "minio://bucket/prefix/42/exports/"), path)
	require.Equal(t, "minio", types.InferStorageFromFilePath(path))
	reader, err := svc.GetFile(ctx, path)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	require.Equal(t, "saved content", string(body))
	copied, err := svc.CopyFile(ctx, path, 42, "new-knowledge")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(copied, "minio://bucket/prefix/42/new-knowledge/"), copied)
	reader, err = svc.GetFile(ctx, copied)
	require.NoError(t, err)
	body, err = io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	require.Equal(t, "saved content", string(body))
	fileURL, err := svc.GetFileURL(ctx, copied)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(fileURL, server.URL+"/bucket/prefix/42/new-knowledge/"), fileURL)
	require.NoError(t, svc.DeleteFile(ctx, copied))
	mu.Lock()
	_, exists := objects[strings.TrimPrefix(copied, "minio://")]
	mu.Unlock()
	require.False(t, exists)
}

// TestMinioSSLControlsSignedURL checks the SSL setting reaches the shared SDK
// endpoint and signed download URL even when the endpoint has no URL scheme.
func TestMinioSSLControlsSignedURL(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for _, tc := range []struct {
		name       string
		useSSL     bool
		wantScheme string
	}{
		{name: "plain", wantScheme: "http://"},
		{name: "ssl", useSSL: true, wantScheme: "https://"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := newS3Client(minioEndpoint("127.0.0.1:9000", tc.useSSL), "ak", "sk", "bucket", "us-east-1", "", true)
			require.NoError(t, err)
			svc.scheme = "minio"
			url, err := svc.GetFileURL(context.Background(), "minio://bucket/object.txt")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(url, tc.wantScheme+"127.0.0.1:9000/bucket/object.txt"), url)
		})
	}
}
