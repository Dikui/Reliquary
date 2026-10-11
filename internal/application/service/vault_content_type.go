package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

// vaultContentTypeChange holds the filesystem side of one user conversion.
// The caller holds articleLock until its PostgreSQL transaction either commits
// or restores this change. Attachments scheduled for removal remain recoverable
// in a private staging directory until that commit succeeds.
type vaultContentTypeChange struct {
	vault              *VaultService
	abs                string
	previous           []byte
	attachmentDir      string
	stagedAttachments  string
	createdAttachments bool
	wroteArticle       bool
}

func (v *VaultService) changeCorrectedArticleBody(ctx context.Context, tx *gorm.DB, k *types.Knowledge, body string, read func(context.Context, string) (io.ReadCloser, error)) (*vaultContentTypeChange, error) {
	if (!k.ContentTypeCorrected && k.ProcessingReviewStatus != types.ProcessingReviewConfirmed) || k.SourceKind != "article" || k.ParseStatus != types.ParseStatusCompleted || k.VaultPath == "" {
		return nil, errors.New("body conversion requires a completed user-reviewed vault article")
	}
	if _, err := uuid.Parse(k.ID); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(filepath.ToSlash(filepath.Clean(k.VaultPath)), "articles/") {
		return nil, errors.New("article file must be inside articles/")
	}
	abs, err := vaultAbsolute(v.root, k.VaultPath)
	if err != nil {
		return nil, err
	}
	previous, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	_, previousBody, err := SplitVaultArticle(previous)
	if err != nil {
		return nil, err
	}
	attachmentDir, err := vaultAbsolute(v.root, filepath.ToSlash(filepath.Join("attachments", k.ID)))
	if err != nil {
		return nil, err
	}
	change := &vaultContentTypeChange{vault: v, abs: abs, previous: previous, attachmentDir: attachmentDir}
	original := ""
	if k.ProcessingMode == types.ProcessingModeSummaryOnly {
		body = ""
		if _, err := os.Stat(attachmentDir); err == nil {
			staged := filepath.Join(v.root, "attachments", ".content-type-"+uuid.NewString())
			if err = os.Rename(attachmentDir, staged); err != nil {
				return change, err
			}
			change.stagedAttachments = staged
		} else if !errors.Is(err, os.ErrNotExist) {
			return change, err
		}
	} else {
		if len(previousBody) != 0 {
			return change, errors.New("only a header-only article can receive its first full body")
		}
		if _, err := os.Stat(attachmentDir); errors.Is(err, os.ErrNotExist) {
			change.createdAttachments = true
		} else if err != nil {
			return change, err
		} else {
			return change, errors.New("header-only article unexpectedly has attachments")
		}
		body, err = v.copyArticleImages(ctx, k, k.VaultPath, body, read)
		if err != nil {
			return change, err
		}
		if k.FilePath != "" {
			if read == nil {
				return change, errors.New("original article reader is unavailable")
			}
			file, err := read(ctx, k.FilePath)
			if err != nil {
				return change, err
			}
			data, readErr := io.ReadAll(file)
			file.Close()
			if readErr != nil {
				return change, readErr
			}
			original = filepath.ToSlash(filepath.Join("attachments", k.ID, vaultOriginalFileName(k.FileName)))
			if err := v.writeAttachment(original, data); err != nil {
				return change, err
			}
		}
	}
	k.VaultBody, k.VaultOriginalPath = body, original
	if err := tx.Model(&types.Knowledge{}).Where("id=? AND space_id=?", k.ID, k.SpaceID).
		Updates(map[string]any{"vault_body": body, "vault_original_path": original}).Error; err != nil {
		return change, err
	}
	transactional := &VaultService{db: tx}
	header, err := transactional.articleHeader(ctx, k)
	if err != nil {
		return change, err
	}
	// A user conversion is the sole call allowed to change the stored body.
	// atomicArticleWrite still verifies that the written body matches exactly.
	if err := v.atomicArticleWrite(abs, RenderVaultArticleHeader(header), []byte(body), nil); err != nil {
		return change, err
	}
	change.wroteArticle = true
	return change, nil
}

func (change *vaultContentTypeChange) rollback() error {
	var failures []error
	if change.wroteArticle {
		header, body, err := SplitVaultArticle(change.previous)
		if err == nil {
			err = change.vault.atomicArticleWrite(change.abs, header, body, nil)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	if change.createdAttachments {
		if err := os.RemoveAll(change.attachmentDir); err != nil {
			failures = append(failures, err)
		}
	}
	if change.stagedAttachments != "" {
		if err := os.Rename(change.stagedAttachments, change.attachmentDir); err != nil {
			failures = append(failures, fmt.Errorf("restore article attachments: %w", err))
		}
	}
	return errors.Join(failures...)
}

func (change *vaultContentTypeChange) finish() error {
	if change.stagedAttachments != "" {
		return os.RemoveAll(change.stagedAttachments)
	}
	return nil
}
