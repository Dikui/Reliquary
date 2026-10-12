package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
)

func (v *VaultService) copyIngestArticleAttachments(ctx context.Context, k *types.Entry, articlePath, body, original string, read func(context.Context, string) (io.ReadCloser, error)) (string, error) {
	attachments := k.IngestAttachmentRecords()
	for i := range attachments {
		item := &attachments[i]
		if item.Path == k.FilePath && original != "" {
			ref, err := filepath.Rel(filepath.Dir(filepath.FromSlash(articlePath)), filepath.FromSlash(original))
			if err != nil {
				return "", err
			}
			body, _ = replaceVaultFileTarget(body, item.Path, filepath.ToSlash(ref))
			item.Path = filepath.ToSlash(ref)
			continue
		}
		if isArticleAttachmentReference(k.ID, articlePath, item.Path) {
			continue
		}
		if read == nil || (!strings.HasPrefix(item.Path, "local://") && !strings.HasPrefix(item.Path, "resource://")) {
			return "", errors.New("ingest attachment requires a local reader")
		}
		file, err := read(ctx, item.Path)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return "", err
		}
		article := *k
		article.VaultPath = articlePath
		ref, err := v.StoreArticleAttachment(&article, item.Name, data)
		if err != nil {
			return "", err
		}
		var replaced bool
		body, replaced = replaceVaultFileTarget(body, item.Path, ref)
		item.Path = ref
		if !replaced {
			body = AppendIngestAttachmentLinks(body, []string{"[" + strings.ReplaceAll(item.Name, "]", "") + "](" + ref + ")"})
		}
	}
	if err := k.SetIngestAttachments(attachments); err != nil {
		return "", err
	}
	return body, nil
}

func replaceVaultFileTarget(body, source, destination string) (string, bool) {
	before := body
	body = strings.ReplaceAll(body, "](<"+source+">)", "](<"+destination+">)")
	body = strings.ReplaceAll(body, "]("+source+")", "]("+destination+")")
	return body, body != before
}

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
	if err := validateImportFileType(ext); err != nil {
		return "", err
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

func replaceVaultImageTargets(body string, mappings map[string]string) string {
	type replacement struct {
		start, end int
		value      string
	}
	var changes []replacement
	for _, ref := range docparser.MarkdownImageReferences(body) {
		value, ok := mappings[ref.URL]
		if !ok {
			continue
		}
		start := ref.Position + strings.Index(body[ref.Position:], ref.URL)
		changes = append(changes, replacement{start, start + len(ref.URL), value})
	}
	for _, match := range searchutil.HTMLImageSrcRegex.FindAllStringSubmatchIndex(body, -1) {
		group := searchutil.HTMLImageSrcURLGroup * 2
		if len(match) <= group+1 || match[group] < 0 {
			continue
		}
		if value, ok := mappings[body[match[group]:match[group+1]]]; ok {
			changes = append(changes, replacement{match[group], match[group+1], value})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].start > changes[j].start })
	for _, change := range changes {
		body = body[:change.start] + change.value + body[change.end:]
	}
	return body
}

func AppendIngestAttachmentLinks(body string, links []string) string {
	if len(links) == 0 {
		return body
	}
	return body + "\n\n" + strings.Join(links, "\n")
}

type vaultArticleSupplement struct {
	vault    *VaultService
	abs      string
	previous []byte
	lock     *sync.Mutex
	closed   bool
}

// BeginArticleSupplement trusts the caller's locked PostgreSQL snapshot and
// keeps the article file lock until the database transaction commits or rolls
// back. It never reads through v.db, which would contend with that transaction.
func (v *VaultService) BeginArticleSupplement(ctx context.Context, k *types.Entry, newBody string, imageMappings map[string]string, attachmentLinks []string, replaceMissingBody bool) (_ *vaultArticleSupplement, supplementErr error) {
	if k == nil {
		return nil, errors.New("article is required")
	}
	defer func() {
		if supplementErr != nil {
			v.recordArticleError(ctx, k.ID, supplementErr)
		}
	}()
	if !k.Incomplete {
		return nil, errors.New("article body is finalized; replacement refused")
	}
	if k.SourceKind != "article" || k.VaultPath == "" {
		return nil, errors.New("supplement requires a published article")
	}
	lock := v.articleLock(k.ID)
	lock.Lock()
	keep := false
	defer func() {
		if !keep {
			lock.Unlock()
		}
	}()
	abs, err := vaultAbsolute(v.root, k.VaultPath)
	if err != nil {
		return nil, err
	}
	previous, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	_, oldBody, err := SplitVaultArticle(previous)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(oldBody, []byte(k.VaultBody)) {
		return nil, errors.New("vault body differs from the locked article snapshot; replacement refused")
	}
	missingImages := map[string]bool{}
	missingBody := false
	for _, missing := range k.IngestMissing() {
		if missing.Type == "image" {
			missingImages[missing.SourceURL] = true
		}
		if missing.Type == "content" {
			missingBody = true
		}
	}
	for source, ref := range imageMappings {
		if !missingImages[source] {
			return nil, errors.New("image replacement does not match recorded missing material")
		}
		if !isArticleAttachmentReference(k.ID, k.VaultPath, ref) {
			return nil, errors.New("supplement image must be a local article attachment")
		}
	}
	for _, link := range attachmentLinks {
		open := strings.LastIndex(link, "](")
		if open < 0 || !strings.HasSuffix(link, ")") || !isArticleAttachmentReference(k.ID, k.VaultPath, strings.Trim(link[open+2:len(link)-1], "<>")) {
			return nil, errors.New("supplement file link must be a local article attachment")
		}
	}
	expected := AppendIngestAttachmentLinks(replaceVaultImageTargets(string(oldBody), imageMappings), attachmentLinks)
	if replaceMissingBody {
		if !missingBody || len(oldBody) != 0 {
			return nil, errors.New("first body requires recorded missing content and an empty stored body")
		}
	} else if newBody != expected {
		return nil, errors.New("supplement changed article text; replacement refused")
	}
	header, _, err := SplitVaultArticle(previous)
	if err != nil {
		return nil, err
	}
	if err := v.atomicArticleWrite(abs, header, []byte(newBody), previous); err != nil {
		return nil, err
	}
	k.VaultBody = newBody
	keep = true
	return &vaultArticleSupplement{vault: v, abs: abs, previous: previous, lock: lock}, nil
}

func (v *VaultService) SupplementArticle(ctx context.Context, k *types.Entry, newBody string, imageMappings map[string]string, attachmentLinks []string, replaceMissingBody bool) error {
	change, err := v.BeginArticleSupplement(ctx, k, newBody, imageMappings, attachmentLinks, replaceMissingBody)
	if err != nil {
		return err
	}
	return change.finish()
}

func (change *vaultArticleSupplement) rollback() error {
	if change == nil || change.closed {
		return nil
	}
	defer change.finish()
	header, body, err := SplitVaultArticle(change.previous)
	if err != nil {
		return err
	}
	return change.vault.atomicArticleWrite(change.abs, header, body, nil)
}
func (change *vaultArticleSupplement) finish() error {
	if change != nil && !change.closed {
		change.closed = true
		change.lock.Unlock()
	}
	return nil
}
