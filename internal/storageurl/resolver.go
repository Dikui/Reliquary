package storageurl

import (
	"context"
	"os"
	"strings"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// LocalStorageBaseDir is the root of the local file storage.
func LocalStorageBaseDir() string {
	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	if baseDir == "" {
		baseDir = "/data/files"
	}
	return baseDir
}

// FileServiceResolver uses the single process file service for local resources.
type FileServiceResolver struct{ fileService interfaces.FileService }

func NewFileServiceResolver(_ *types.Space, fileService interfaces.FileService) *FileServiceResolver {
	return &FileServiceResolver{fileService: fileService}
}

func (r *FileServiceResolver) WithContext(_ context.Context) *FileServiceResolver { return r }

func (r *FileServiceResolver) ResolveFileService(filePath string) interfaces.FileService {
	if _, ok := types.ParseResourcePath(filePath); ok {
		return r.fileService
	}
	if strings.HasPrefix(filePath, "local://") {
		return r.fileService
	}
	return nil
}

var _ Resolver = (*FileServiceResolver)(nil)
