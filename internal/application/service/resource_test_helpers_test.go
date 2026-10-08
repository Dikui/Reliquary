package service

import (
	"context"
	"io"
	"mime/multipart"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"time"
)

type fakeFileService struct {
	saved     map[string][]byte
	saveErr   error
	seq       int
	tenantIDs []uint64
}

func (f *fakeFileService) CheckConnectivity(_ context.Context) error { return nil }

func (f *fakeFileService) SaveFile(_ context.Context, _ *multipart.FileHeader, _ uint64, _ string) (string, error) {
	panic("SaveFile unexpected file operation")
}

func (f *fakeFileService) SaveBytes(_ context.Context, data []byte, tenantID uint64, fileName string, _ bool) (string, error) {
	if f.saveErr != nil {
		return "", f.saveErr
	}
	if f.saved == nil {
		f.saved = map[string][]byte{}
	}
	f.seq++
	key := "fake://tenant-" + fileName
	f.saved[key] = append([]byte(nil), data...)
	f.tenantIDs = append(f.tenantIDs, tenantID)
	return key, nil
}

func (f *fakeFileService) GetFile(_ context.Context, _ string) (io.ReadCloser, error) {
	panic("GetFile unexpected file operation")
}

func (f *fakeFileService) GetFileURL(_ context.Context, _ string) (string, error) {
	panic("GetFileURL unexpected file operation")
}

func (f *fakeFileService) DeleteFile(_ context.Context, _ string) error { return nil }

func (f *fakeFileService) CopyFile(_ context.Context, _ string, _ uint64, _ string) (string, error) {
	panic("CopyFile unexpected file operation")
}

// bindCall records one Bind invocation for assertions.
type bindCall struct {
	ref       string
	ownerType string
	ownerID   string
	relation  string
}

// fakeCatalog captures resource ownership calls; other methods are a
// no-op stub for resource ownership tests.
type fakeCatalog struct {
	binds   []bindCall
	bindErr error
	// Release scripting, used by the deletion-guard tests. releaseRemaining
	// maps a reference to the binding count left after the release; a missing
	// entry answers -1 ("not a catalog handle").
	releaseRemaining map[string]int64
	releaseErr       error
	releases         []string
}

func (c *fakeCatalog) Register(context.Context, uint64, string, interfaces.ResourceRegistration) (string, error) {
	return "", nil
}
func (c *fakeCatalog) Resolve(context.Context, string) (*types.StoredResource, error) {
	return nil, nil
}
func (c *fakeCatalog) ResolvePath(_ context.Context, v string) (string, *types.StoredResource, error) {
	return v, nil, nil
}
func (c *fakeCatalog) Bind(_ context.Context, ref, ownerType, ownerID, relation string) error {
	c.binds = append(c.binds, bindCall{ref, ownerType, ownerID, relation})
	return c.bindErr
}
func (c *fakeCatalog) MarkDeleted(context.Context, string) error { return nil }

func (c *fakeCatalog) Release(_ context.Context, ref, ownerType, ownerID string) (int64, error) {
	c.releases = append(c.releases, ref+"|"+ownerType+"|"+ownerID)
	if c.releaseErr != nil {
		return -1, c.releaseErr
	}
	if remaining, ok := c.releaseRemaining[ref]; ok {
		return remaining, nil
	}
	return -1, nil
}
func (c *fakeCatalog) CreateAccessGrant(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (c *fakeCatalog) ResolveAccessGrant(context.Context, string) (*types.StoredResource, error) {
	return nil, nil
}
