package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"reliquary/internal/types"
)

func ingestCompletionPending(k *types.Entry) bool {
	if k == nil {
		return false
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(k.Metadata, &metadata) != nil {
		return false
	}
	var pending bool
	_ = json.Unmarshal(metadata["completion_pending"], &pending)
	return pending
}

func (v *VaultService) prepareArticleMaterials(ctx context.Context, k *types.Entry, articlePath, body string, read func(context.Context, string) (io.ReadCloser, error)) (string, string, error) {
	body, err := v.copyArticleImages(ctx, k, articlePath, body, read)
	if err != nil {
		return "", "", err
	}
	original := ""
	if k.FilePath != "" {
		if read == nil {
			return "", "", errors.New("original file reader is unavailable")
		}
		file, err := read(ctx, k.FilePath)
		if err != nil {
			return "", "", fmt.Errorf("read original article file: %w", err)
		}
		data, readErr := io.ReadAll(file)
		file.Close()
		if readErr != nil {
			return "", "", readErr
		}
		original = filepath.ToSlash(filepath.Join("attachments", k.ID, vaultOriginalFileName(k.FileName)))
		if err = v.writeAttachment(original, data); err != nil {
			return "", "", err
		}
	}
	if isIngestArticle(k) {
		body, err = v.copyIngestArticleAttachments(ctx, k, articlePath, body, original, read)
		if err != nil {
			return "", "", err
		}
	}
	return body, original, nil
}

// Only a fully supplied completion awaiting its new processing decision may
// publish the first body of a previously header-only article. The body bytes
// in both PostgreSQL and the file must still be empty.
func (v *VaultService) publishCompletedHeaderOnlyArticle(ctx context.Context, k *types.Entry, body string, read func(context.Context, string) (io.ReadCloser, error)) (publishErr error) {
	if k.Incomplete || k.VaultBody != "" {
		return errors.New("completion publication requires a complete header-only article")
	}
	abs, err := vaultAbsolute(v.root, k.VaultPath)
	if err != nil {
		return err
	}
	previous, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	_, oldBody, err := SplitVaultArticle(previous)
	if err != nil {
		return err
	}
	if len(oldBody) != 0 {
		return errors.New("header-only completion has a changed body; replacement refused")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
		return err
	}
	delete(metadata, "completion_pending")
	updatedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	attachments, err := vaultAbsolute(v.root, filepath.ToSlash(filepath.Join("attachments", k.ID)))
	if err != nil {
		return err
	}
	previousFiles := map[string]bool{}
	if files, err := os.ReadDir(attachments); err == nil {
		for _, file := range files {
			previousFiles[file.Name()] = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	written := false
	defer func() {
		if publishErr == nil {
			return
		}
		if written {
			header, old, splitErr := SplitVaultArticle(previous)
			if splitErr == nil {
				splitErr = v.atomicArticleWrite(abs, header, old, nil)
			}
			publishErr = errors.Join(publishErr, splitErr)
		}
		files, _ := os.ReadDir(attachments)
		for _, file := range files {
			if !previousFiles[file.Name()] {
				publishErr = errors.Join(publishErr, os.Remove(filepath.Join(attachments, file.Name())))
			}
		}
	}()
	original := ""
	if k.ProcessingMode == types.ProcessingModeSummaryOnly {
		body = ""
	} else {
		body, original, err = v.prepareArticleMaterials(ctx, k, k.VaultPath, body, read)
		if err != nil {
			return err
		}
	}
	header, err := v.articleHeader(ctx, k)
	if err != nil {
		return err
	}
	if err := v.atomicArticleWrite(abs, RenderVaultArticleHeader(header), []byte(body), previous); err != nil {
		return err
	}
	written = true
	values := map[string]any{"vault_body": body, "vault_original_path": original, "ingest_attachments": k.IngestAttachments, "metadata": types.JSON(updatedMetadata)}
	if err := v.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", k.ID, k.SpaceID).Updates(values).Error; err != nil {
		return err
	}
	k.VaultBody, k.VaultOriginalPath, k.Metadata = body, original, types.JSON(updatedMetadata)
	return nil
}
