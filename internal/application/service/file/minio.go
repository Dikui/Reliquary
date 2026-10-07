package file

import (
	"context"
	"strings"

	"reliquary/internal/types/interfaces"
)

func minioEndpoint(endpoint string, useSSL bool) string {
	if strings.Contains(endpoint, "://") {
		return endpoint
	}
	if useSSL {
		return "https://" + endpoint
	}
	return "http://" + endpoint
}

// NewMinioFileService uses the S3-compatible implementation for MinIO.
func NewMinioFileService(endpoint, accessKeyID, secretAccessKey, bucketName string, useSSL bool) (interfaces.FileService, error) {
	return newMinioFileService(endpoint, accessKeyID, secretAccessKey, bucketName, "", useSSL)
}

// CheckMinioConnectivity checks the bucket through the S3-compatible client.
func CheckMinioConnectivity(ctx context.Context, endpoint, accessKeyID, secretAccessKey, bucketName string, useSSL bool) error {
	return CheckS3ConnectivityWithOptions(ctx, minioEndpoint(endpoint, useSSL), accessKeyID, secretAccessKey, bucketName, "us-east-1", true)
}

// newMinioFileService preserves the MinIO storage identity while sharing S3 operations.
func newMinioFileService(endpoint, accessKeyID, secretAccessKey, bucketName, pathPrefix string, useSSL bool) (interfaces.FileService, error) {
	svc, err := NewS3FileServiceWithOptions(minioEndpoint(endpoint, useSSL), accessKeyID, secretAccessKey, bucketName, "us-east-1", pathPrefix, true)
	if err != nil {
		return nil, err
	}
	svc.(*s3FileService).scheme = "minio"
	return svc, nil
}
