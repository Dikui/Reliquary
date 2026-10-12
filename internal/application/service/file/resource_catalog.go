package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// resourceCatalogFileService keeps provider drivers physical-path-only while
// exposing stable resource:// references to the application layer.
type resourceCatalogFileService struct {
	inner       interfaces.FileService
	catalog     interfaces.ResourceCatalog
	externalURL string
	onStored    func(string)
}

// NewResourceCatalogFileService decorates a physical FileService with stable
// resource registration and resolution.
func NewResourceCatalogFileService(
	inner interfaces.FileService,
	catalog interfaces.ResourceCatalog,
) interfaces.FileService {
	if inner == nil || catalog == nil {
		return inner
	}
	return &resourceCatalogFileService{
		inner:       inner,
		catalog:     catalog,
		externalURL: strings.TrimRight(strings.TrimSpace(os.Getenv("APP_EXTERNAL_URL")), "/"),
	}
}

// WithResourceCatalog binds a decorated file service to the caller's catalog.
// onStored tracks new physical objects for cleanup if its database transaction
// rolls back. Provider drivers and unrelated file-service fixtures are reused.
func WithResourceCatalog(svc interfaces.FileService, catalog interfaces.ResourceCatalog, onStored func(string)) interfaces.FileService {
	if decorated, ok := svc.(*resourceCatalogFileService); ok {
		copy := *decorated
		copy.catalog, copy.onStored = catalog, onStored
		return &copy
	}
	return svc
}

func (s *resourceCatalogFileService) CheckConnectivity(ctx context.Context) error {
	return s.inner.CheckConnectivity(ctx)
}

func resourceKind(name string) (string, string) {
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	kind := "file"
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		kind = "image"
	case strings.HasPrefix(mimeType, "audio/"):
		kind = "audio"
	case strings.HasPrefix(mimeType, "video/"):
		kind = "video"
	}
	return kind, mimeType
}

func (s *resourceCatalogFileService) register(
	ctx context.Context,
	physical string,
	spaceID uint64,
	name string,
	size int64,
	temporary bool,
	contentHash string,
) (string, error) {
	if s.onStored != nil {
		s.onStored(physical)
	}
	kind, mimeType := resourceKind(name)
	ref, err := s.catalog.Register(ctx, spaceID, physical, interfaces.ResourceRegistration{
		Kind:         kind,
		MimeType:     mimeType,
		OriginalName: filepath.Base(name),
		Size:         size,
		ContentHash:  contentHash,
		Temporary:    temporary,
	})
	if err != nil {
		_ = s.inner.DeleteFile(ctx, physical)
		return "", fmt.Errorf("register stored resource: %w", err)
	}
	return ref, nil
}

func (s *resourceCatalogFileService) SaveFile(
	ctx context.Context,
	file *multipart.FileHeader,
	spaceID uint64,
	entryID string,
) (string, error) {
	physical, err := s.inner.SaveFile(ctx, file, spaceID, entryID)
	if err != nil {
		return "", err
	}
	ref, err := s.register(ctx, physical, spaceID, file.Filename, file.Size, false, "")
	if err != nil {
		return "", err
	}
	if entryID != "" {
		if err := s.catalog.Bind(ctx, ref, types.ResourceOwnerEntry, entryID, types.ResourceRelationSourceFile); err != nil {
			_ = s.DeleteFile(ctx, ref)
			return "", fmt.Errorf("bind stored resource: %w", err)
		}
	}
	return ref, nil
}

func (s *resourceCatalogFileService) SaveBytes(
	ctx context.Context,
	data []byte,
	spaceID uint64,
	fileName string,
	temp bool,
) (string, error) {
	physical, err := s.inner.SaveBytes(ctx, data, spaceID, fileName, temp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return s.register(ctx, physical, spaceID, fileName, int64(len(data)), temp, hex.EncodeToString(sum[:]))
}

func (s *resourceCatalogFileService) resolve(ctx context.Context, value string) (string, bool, error) {
	physical, resource, err := s.catalog.ResolvePath(ctx, value)
	return physical, resource != nil, err
}

func (s *resourceCatalogFileService) GetFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	physical, _, err := s.resolve(ctx, filePath)
	if err != nil {
		return nil, err
	}
	return s.inner.GetFile(ctx, physical)
}

func (s *resourceCatalogFileService) GetFileURL(ctx context.Context, filePath string) (string, error) {
	physical, isResource, err := s.resolve(ctx, filePath)
	if err != nil {
		return "", err
	}
	if isResource && s.externalURL != "" {
		token, grantErr := s.catalog.CreateAccessGrant(ctx, filePath, 2*time.Hour)
		if grantErr != nil {
			return "", grantErr
		}
		return s.externalURL + "/r/" + token, nil
	}
	return s.inner.GetFileURL(ctx, physical)
}

func (s *resourceCatalogFileService) DeleteFile(ctx context.Context, filePath string) error {
	physical, isResource, err := s.resolve(ctx, filePath)
	if err != nil {
		return err
	}
	if err := s.inner.DeleteFile(ctx, physical); err != nil {
		return err
	}
	if isResource {
		return s.catalog.MarkDeleted(ctx, filePath)
	}
	return nil
}

func (s *resourceCatalogFileService) CopyFile(
	ctx context.Context,
	filePath string,
	spaceID uint64,
	entryID string,
) (string, error) {
	physical, _, err := s.resolve(ctx, filePath)
	if err != nil {
		return "", err
	}
	copied, err := s.inner.CopyFile(ctx, physical, spaceID, entryID)
	if err != nil {
		return "", err
	}
	ref, err := s.register(ctx, copied, spaceID, filepath.Base(physical), 0, false, "")
	if err != nil {
		return "", err
	}
	if entryID != "" {
		if err := s.catalog.Bind(ctx, ref, types.ResourceOwnerEntry, entryID, types.ResourceRelationSourceFile); err != nil {
			_ = s.DeleteFile(ctx, ref)
			return "", fmt.Errorf("bind copied resource: %w", err)
		}
	}
	return ref, nil
}

var _ interfaces.FileService = (*resourceCatalogFileService)(nil)
