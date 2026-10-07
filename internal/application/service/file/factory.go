package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// NewFileServiceFromStorageConfig builds a provider-specific FileService from tenant storage config.
// provider can be empty; in that case it falls back to sec.DefaultProvider.
// Returns the resolved provider name together with the service.
func NewFileServiceFromStorageConfig(
	provider string,
	sec *types.StorageEngineConfig,
	localBaseDir string,
) (interfaces.FileService, string, error) {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" && sec != nil {
		p = strings.ToLower(strings.TrimSpace(sec.DefaultProvider))
	}
	if p == "" {
		return nil, "", fmt.Errorf("empty provider")
	}

	if localBaseDir == "" {
		localBaseDir = strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	}
	if localBaseDir == "" {
		localBaseDir = "/data/files"
	}

	switch p {
	case "local":
		baseDir := localBaseDir
		if sec != nil && sec.Local != nil {
			rawPrefix := strings.TrimSpace(sec.Local.PathPrefix)
			prefix := strings.Trim(rawPrefix, "/\\")
			if prefix != "" {
				candidate := filepath.Join(baseDir, prefix)
				if safeBaseDir, err := secutils.SafePathUnderBase(baseDir, candidate); err == nil {
					baseDir = safeBaseDir
				}
			}
		}
		externalURL := strings.TrimSpace(os.Getenv("APP_EXTERNAL_URL"))
		return NewLocalFileService(baseDir, externalURL), p, nil

	case "minio":
		if sec == nil || sec.MinIO == nil {
			return nil, p, fmt.Errorf("missing minio config")
		}
		var endpoint, accessKeyID, secretAccessKey string
		if sec.MinIO.Mode == "remote" {
			endpoint = strings.TrimSpace(sec.MinIO.Endpoint)
			accessKeyID = strings.TrimSpace(sec.MinIO.AccessKeyID)
			secretAccessKey = strings.TrimSpace(sec.MinIO.SecretAccessKey)
		} else {
			endpoint = strings.TrimSpace(os.Getenv("MINIO_ENDPOINT"))
			accessKeyID = strings.TrimSpace(os.Getenv("MINIO_ACCESS_KEY_ID"))
			secretAccessKey = strings.TrimSpace(os.Getenv("MINIO_SECRET_ACCESS_KEY"))
		}
		bucketName := strings.TrimSpace(sec.MinIO.BucketName)
		if bucketName == "" {
			bucketName = strings.TrimSpace(os.Getenv("MINIO_BUCKET_NAME"))
		}
		if endpoint == "" || accessKeyID == "" || secretAccessKey == "" || bucketName == "" {
			return nil, p, fmt.Errorf("incomplete minio config")
		}
		svc, err := newMinioFileService(endpoint, accessKeyID, secretAccessKey, bucketName, sec.MinIO.PathPrefix, sec.MinIO.UseSSL)
		return svc, p, err

	case "s3":
		if sec == nil || sec.S3 == nil || sec.S3.Region == "" || sec.S3.BucketName == "" || (sec.S3.AccessKey == "") != (sec.S3.SecretKey == "") {
			return nil, p, fmt.Errorf("incomplete s3 config")
		}
		pathPrefix := strings.TrimSpace(sec.S3.PathPrefix)
		if pathPrefix == "" {
			pathPrefix = "reliquary/"
		}
		svc, err := NewS3FileServiceWithOptions(sec.S3.Endpoint, sec.S3.AccessKey, sec.S3.SecretKey, sec.S3.BucketName, sec.S3.Region, pathPrefix, sec.S3.ForcePathStyle)
		return svc, p, err

	default:
		return nil, p, fmt.Errorf("unsupported provider %q", p)
	}
}
