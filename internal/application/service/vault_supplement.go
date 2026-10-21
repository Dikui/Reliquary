package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/types"
)

func isArticleAttachmentReference(id, articlePath, ref string) bool {
	if ref == "" || filepath.IsAbs(ref) || strings.Contains(ref, "\\") || strings.Contains(ref, "://") {
		return false
	}
	path := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(articlePath)), filepath.FromSlash(ref))))
	return strings.HasPrefix(path, "attachments/"+id+"/")
}

// StoreArticleAttachment returns a link relative to the article. Every call
// creates its own file so a failed transaction cannot remove an older copy.
func (v *VaultService) StoreArticleAttachment(k *types.Entry, name string, data []byte) (string, error) {
	if k == nil || k.VaultPath == "" || len(data) == 0 {
		return "", errors.New("article and attachment bytes are required")
	}
	ext := strings.ToLower(filepath.Ext(filepath.Base(name)))
	if !docparser.IsImageFormat(ext) || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return "", errors.New("only image attachments are accepted")
	}
	hash := sha256.Sum256(data)
	attachment := filepath.ToSlash(filepath.Join("attachments", k.ID, fmt.Sprintf("asset-%x-%s%s", hash[:8], uuid.NewString()[:8], ext)))
	if err := v.writeAttachment(attachment, data); err != nil {
		return "", err
	}
	relative, err := filepath.Rel(filepath.Dir(filepath.FromSlash(k.VaultPath)), filepath.FromSlash(attachment))
	return filepath.ToSlash(relative), err
}

func (v *VaultService) DeleteStoredArticleAttachment(k *types.Entry, ref string) error {
	if k == nil || !isArticleAttachmentReference(k.ID, k.VaultPath, ref) {
		return errors.New("attachment must belong to the article")
	}
	relative := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(k.VaultPath)), filepath.FromSlash(ref))))
	abs, err := vaultAbsolute(v.root, relative)
	if err != nil {
		return err
	}
	if err = os.Remove(abs); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type vaultArticleContentUpdate struct {
	lock   *sync.Mutex
	closed bool
}

// Only the explicit content-update API may replace finalized text. Keep the
// article lock until its database transaction commits or rolls back.
func (v *VaultService) beginArticleContentUpdate(k *types.Entry, newBody string) (*vaultArticleContentUpdate, error) {
	lock := v.articleLock(k.ID)
	lock.Lock()
	keep := false
	defer func() {
		if !keep {
			lock.Unlock()
		}
	}()
	k.VaultBody = newBody
	keep = true
	return &vaultArticleContentUpdate{lock: lock}, nil
}

func (change *vaultArticleContentUpdate) rollback() error {
	return change.finish()
}

func (change *vaultArticleContentUpdate) finish() error {
	if change != nil && !change.closed {
		change.closed = true
		change.lock.Unlock()
	}
	return nil
}
