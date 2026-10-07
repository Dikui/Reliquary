package service

import (
	"context"
	"testing"

	"reliquary/internal/types"
)

// TestBuildStorageConfig_TenantMergeAllProviders pins the tenant-merge branch
// of (knowledgeService).buildStorageConfig: every provider listed in
// types.StorageEngineConfig and types.ParseProviderScheme must produce a
// fully-populated DocParserStorageConfig when the tenant carries the matching
// engine config. Each retained provider must keep the credentials and endpoint
// needed by the document reader.
func TestBuildStorageConfig_TenantMergeAllProviders(t *testing.T) {
	t.Parallel()

	// Each table entry sets one engine config on a fresh tenant and checks that
	// the merged DocParserStorageConfig surfaces the values the docreader needs.
	type want struct {
		provider        string // expected DocParserStorageConfig.Provider (uppercase)
		region          string
		bucket          string
		accessKeyID     string
		secretAccessKey string
		pathPrefix      string
		endpoint        string
	}

	tests := []struct {
		name     string
		kbConfig *types.StorageProviderConfig
		tenant   *types.Tenant
		want     want
	}{
		{
			name:     "local — only path prefix",
			kbConfig: &types.StorageProviderConfig{Provider: "local"},
			tenant: &types.Tenant{StorageEngineConfig: &types.StorageEngineConfig{
				Local: &types.LocalEngineConfig{PathPrefix: "/data/wk"},
			}},
			want: want{provider: "LOCAL", pathPrefix: "/data/wk"},
		},
		{
			name:     "minio remote — full credentials",
			kbConfig: &types.StorageProviderConfig{Provider: "minio"},
			tenant: &types.Tenant{StorageEngineConfig: &types.StorageEngineConfig{
				MinIO: &types.MinIOEngineConfig{
					Mode:            "remote",
					Endpoint:        "minio.example.com:9000",
					AccessKeyID:     "minio-ak",
					SecretAccessKey: "minio-sk",
					BucketName:      "kb-minio",
					PathPrefix:      "wk/",
				},
			}},
			want: want{
				provider: "MINIO", endpoint: "minio.example.com:9000",
				accessKeyID: "minio-ak", secretAccessKey: "minio-sk",
				bucket: "kb-minio", pathPrefix: "wk/",
			},
		},
		{
			name:     "s3 — issue #1117 regression guard",
			kbConfig: &types.StorageProviderConfig{Provider: "s3"},
			tenant: &types.Tenant{StorageEngineConfig: &types.StorageEngineConfig{
				S3: &types.S3EngineConfig{
					Endpoint: "s3.us-east-1.amazonaws.com", Region: "us-east-1",
					AccessKey: "AKIA...", SecretKey: "s3-sk",
					BucketName: "kb-s3", PathPrefix: "wk/",
				},
			}},
			want: want{
				provider: "S3", endpoint: "s3.us-east-1.amazonaws.com", region: "us-east-1",
				accessKeyID: "AKIA...", secretAccessKey: "s3-sk",
				bucket: "kb-s3", pathPrefix: "wk/",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kb := &types.KnowledgeBase{
				ID:                    "kb-" + tt.want.provider,
				StorageProviderConfig: tt.kbConfig,
			}
			ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, tt.tenant)

			s := &knowledgeService{}
			got := s.buildStorageConfig(ctx, kb)
			if got == nil {
				t.Fatalf("buildStorageConfig returned nil")
			}
			if got.Provider != tt.want.provider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.want.provider)
			}
			if got.Region != tt.want.region {
				t.Errorf("Region = %q, want %q", got.Region, tt.want.region)
			}
			if got.BucketName != tt.want.bucket {
				t.Errorf("BucketName = %q, want %q", got.BucketName, tt.want.bucket)
			}
			if got.AccessKeyID != tt.want.accessKeyID {
				t.Errorf("AccessKeyID = %q, want %q", got.AccessKeyID, tt.want.accessKeyID)
			}
			if got.SecretAccessKey != tt.want.secretAccessKey {
				t.Errorf("SecretAccessKey = %q, want %q", got.SecretAccessKey, tt.want.secretAccessKey)
			}
			if got.PathPrefix != tt.want.pathPrefix {
				t.Errorf("PathPrefix = %q, want %q", got.PathPrefix, tt.want.pathPrefix)
			}
			if got.Endpoint != tt.want.endpoint {
				t.Errorf("Endpoint = %q, want %q", got.Endpoint, tt.want.endpoint)
			}
		})
	}
}

// TestBuildStorageConfig_LegacyPathOnlyForMinIO verifies that the legacy
// (kb.StorageConfig "cos_config" column) path is only used for the providers
// that historically used it. Local and S3 resolve through tenant settings.
func TestBuildStorageConfig_LegacyPathOnlyForMinIO(t *testing.T) {
	t.Parallel()

	// Legacy MinIO parameters are preserved; other providers use tenant settings.
	legacy := types.StorageConfig{
		SecretID:   "legacy-sid",
		SecretKey:  "legacy-sk",
		Region:     "ap-guangzhou",
		BucketName: "legacy-bucket",
		PathPrefix: "wk/",
	}

	tests := []struct {
		name        string
		provider    string
		wantBucket  string // populated when legacy path was taken
		wantSecret  string
		wantTenants bool // true when the test should fall through to tenant merge (empty result)
	}{
		{name: "minio uses legacy", provider: "minio", wantBucket: "legacy-bucket", wantSecret: "legacy-sid"},
		{name: "local skips legacy", provider: "local", wantTenants: true},
		{name: "s3 skips legacy", provider: "s3", wantTenants: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			kb := &types.KnowledgeBase{
				ID:                    "kb-legacy-" + tt.provider,
				StorageConfig:         legacy,
				StorageProviderConfig: &types.StorageProviderConfig{Provider: tt.provider},
			}
			ctx := context.Background() // no tenant config → tenant-merge yields empty

			s := &knowledgeService{}
			got := s.buildStorageConfig(ctx, kb)
			if got == nil {
				t.Fatalf("buildStorageConfig returned nil")
			}

			if tt.wantTenants {
				// Legacy path NOT taken → fields stay empty (Provider is the only thing set).
				if got.BucketName != "" || got.AccessKeyID != "" {
					t.Errorf("provider=%s expected legacy path skipped, got bucket=%q ak=%q",
						tt.provider, got.BucketName, got.AccessKeyID)
				}
				return
			}
			if got.BucketName != tt.wantBucket {
				t.Errorf("BucketName = %q, want %q", got.BucketName, tt.wantBucket)
			}
			if got.AccessKeyID != tt.wantSecret {
				t.Errorf("AccessKeyID (mapped from legacy SecretID) = %q, want %q", got.AccessKeyID, tt.wantSecret)
			}
		})
	}
}

// TestBuildStorageConfig_NoTenantFallsThroughToEmpty pins the no-tenant case:
// when neither the kb nor the context carry storage info, the result is an
// empty config tagged with the requested provider — matches the "fall back to
// global default" intent rather than panicking.
func TestBuildStorageConfig_NoTenantFallsThroughToEmpty(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"local", "minio", "s3"} {
		p := p
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			kb := &types.KnowledgeBase{
				ID:                    "kb-empty-" + p,
				StorageProviderConfig: &types.StorageProviderConfig{Provider: p},
			}
			s := &knowledgeService{}
			got := s.buildStorageConfig(context.Background(), kb)
			if got == nil {
				t.Fatalf("buildStorageConfig returned nil for %s", p)
			}
			// Provider tag is uppercase per the implementation contract.
			if got.Provider == "" {
				t.Errorf("Provider tag missing for %s", p)
			}
		})
	}
}
