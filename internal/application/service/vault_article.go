package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"

	"github.com/google/uuid"
)

// ArticleHeaderRefresher is deliberately separate from EntryService so
// callers for field changes do not need to depend on the ingestion interface.
type ArticleHeaderRefresher interface {
	ScheduleRefresh(context.Context, string)
}

type vaultArticleState struct {
	mu              sync.Mutex
	locks           sync.Map
	timers          map[string]*time.Timer
	stopped         bool
	wg              sync.WaitGroup
	recentError     string
	writeCount      int
	beforeTempWrite func([]byte) []byte
}

func (v *VaultService) articleLock(id string) *sync.Mutex {
	lock, _ := v.articles.locks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// vaultAbsolute rejects escaping paths and symbolic links beneath the root.
// The configured root itself may be a symbolic link to a local volume.
func vaultAbsolute(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("vault path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("vault path escapes the root")
	}
	current := root
	for _, segment := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("vault path contains a symbolic link: %s", relative)
		}
	}
	return filepath.Join(root, clean), nil
}

func (v *VaultService) articleHeader(ctx context.Context, k *types.Entry) (VaultArticleHeader, error) {
	source, err := entrySourceInfo(k)
	if err != nil {
		return VaultArticleHeader{}, err
	}
	url := source.OriginalURL
	if url == "" && (k.Type == "url" || k.Type == "file_url") {
		url = k.Source
	}
	h := VaultArticleHeader{ID: k.ID, URL: url, Title: k.Title, Publisher: source.Publisher, Author: source.Author,
		PublishedAt: canonicalVaultDate(source.PublishedAt), CollectedAt: k.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		ContentType: k.ContentType, Summary: k.StructuredSummary, Starred: k.Starred, CollectReason: k.CollectReason}
	var library types.Library
	if err := v.db.WithContext(ctx).First(&library, "id=?", k.LibraryID).Error; err != nil {
		return h, err
	}
	h.LibraryID, h.Library = library.ID, library.Name
	h.SourceRef, h.Identifier, h.Graph = k.SourceRef, k.Identifier, k.Graph
	h.HasUpdate, h.LatestEntryID = k.HasUpdate, k.LatestEntryID
	if k.SourceRef != "" && h.LatestEntryID == "" {
		h.LatestEntryID = k.ID
	}
	var metadata struct {
		RelatedArticleIDs []string `json:"related_article_ids"`
	}
	if err := json.Unmarshal(k.Metadata, &metadata); err == nil {
		h.RelatedArticleIDs = metadata.RelatedArticleIDs
	}
	h.SourceUnverified = k.SourceUnverified
	if k.SourceKind == "mine" {
		h.SourceKind = "mine"
	}
	h.Acquisition = k.Acquisition
	if k.SourceUnverified {
		h.URL = ""
	}
	var names []string
	if err = v.db.WithContext(ctx).Table("entities e").Select("e.canonical_name").
		Joins("JOIN entry_entities links ON links.entity_id=e.id AND links.space_id=e.space_id").
		Where("links.entry_id=? AND links.space_id=?", k.ID, k.SpaceID).Pluck("e.canonical_name", &names).Error; err != nil {
		return h, err
	}
	for _, name := range names {
		h.Entities = append(h.Entities, "[["+name+"]]")
	}
	if err = v.db.WithContext(ctx).Table("topics t").Select("t.name").
		Joins("JOIN entry_topics links ON links.topic_id=t.id AND links.space_id=t.space_id").
		Where("links.entry_id=? AND links.space_id=?", k.ID, k.SpaceID).Pluck("t.name", &h.Topics).Error; err != nil {
		return h, err
	}
	return h, nil
}

func canonicalVaultDate(value string) string {
	value = strings.TrimSpace(value)
	if date, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return date.UTC().Format(time.RFC3339Nano)
	}
	if date, err := time.Parse("2006-01-02", value); err == nil {
		return date.Format("2006-01-02")
	}
	return value
}

// WriteArticle publishes approved material and verifies existing body bytes.
// A completed header-only intake may publish its first body after processing.
func (v *VaultService) WriteArticle(ctx context.Context, k *types.Entry, body string, read func(context.Context, string) (io.ReadCloser, error)) error {
	if !isIngestArticle(k) {
		return errors.New("only approved material can be written to the file library")
	}
	if _, err := uuid.Parse(k.ID); err != nil {
		return fmt.Errorf("article id must be a UUID: %w", err)
	}
	lock := v.articleLock(k.ID)
	lock.Lock()
	defer lock.Unlock()
	// Even the initial header comes from PostgreSQL. In-memory creation times
	// may retain nanoseconds that were rounded to microseconds by PostgreSQL.
	var persisted types.Entry
	if err := v.db.WithContext(ctx).Where("id=? AND space_id=?", k.ID, k.SpaceID).First(&persisted).Error; err != nil {
		return err
	}
	*k = persisted
	if !isIngestArticle(k) || k.ParseStatus != types.ParseStatusCompleted {
		return errors.New("article must complete processing before vault write")
	}
	if intake, err := k.IntakeMetadata(); err != nil {
		return err
	} else if intake != nil && intake.Decision != "approved" {
		return errors.New("article must pass intake review before vault write")
	}
	if k.VaultPath != "" {
		if isIngestArticle(k) && body != k.VaultBody {
			err := errors.New("published ingest article body differs from its stored body; replacement refused")
			v.recordArticleError(ctx, k.ID, err)
			return err
		}
		v.ScheduleRefresh(ctx, k.ID)
		return nil
	}
	h, err := v.articleHeader(ctx, k)
	if err != nil {
		return err
	}
	directory, err := vaultLibraryDirectory(v.db.WithContext(ctx), k.LibraryID)
	if err != nil {
		return err
	}
	relative := vaultEntryPath(directory, k.Title, k.ID)
	abs, err := vaultAbsolute(v.root, relative)
	if err != nil {
		return err
	}
	if _, err = os.Stat(abs); err == nil {
		return fmt.Errorf("article vault file already exists: %s", relative)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body, err = v.prepareArticleMaterials(ctx, k, relative, body, read)
	if err != nil {
		return err
	}
	if err = v.atomicArticleWrite(abs, RenderVaultArticleHeader(h), []byte(body), nil); err != nil {
		return err
	}
	kind := k.SourceKind
	if kind == "" {
		kind = "article"
	}
	values := map[string]any{"source_kind": kind, "vault_path": relative, "vault_body": body}

	if err = v.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", k.ID, k.SpaceID).Updates(values).Error; err != nil {
		_ = os.Remove(abs)
		return err
	}
	k.SourceKind, k.VaultPath, k.VaultBody = kind, relative, body
	if k.SourceRef != "" {
		var ids []string
		if err := v.db.WithContext(ctx).Model(&types.Entry{}).Where("space_id=? AND library_id=? AND source_ref=? AND id<>? AND vault_path<>''", k.SpaceID, k.LibraryID, k.SourceRef, k.ID).Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			v.ScheduleRefresh(ctx, id)
		}
	}
	return nil
}

func (v *VaultService) writeAttachment(relative string, data []byte) error {
	abs, err := vaultAbsolute(v.root, relative)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0644)
}

func (v *VaultService) copyArticleImages(ctx context.Context, k *types.Entry, articlePath, body string, read func(context.Context, string) (io.ReadCloser, error)) (string, error) {
	type replacement struct {
		start, end int
		url        string
	}
	var replacements []replacement
	for _, ref := range docparser.MarkdownImageReferences(body) {
		start := ref.Position + strings.Index(body[ref.Position:], ref.URL)
		replacements = append(replacements, replacement{start, start + len(ref.URL), ref.URL})
	}
	for _, match := range searchutil.HTMLImageSrcRegex.FindAllStringSubmatchIndex(body, -1) {
		group := searchutil.HTMLImageSrcURLGroup * 2
		if len(match) > group+1 && match[group] >= 0 {
			replacements = append(replacements, replacement{match[group], match[group+1], body[match[group]:match[group+1]]})
		}
	}
	stored := map[string]string{}
	for _, item := range replacements {
		if _, ok := stored[item.url]; ok {
			continue
		}
		if remoteIngestImage(item.url) {
			stored[item.url] = item.url
			continue
		}
		// A supplement may retain images already copied into this article's
		// own attachment directory. Those bytes are already authoritative.
		if isArticleAttachmentReference(k.ID, articlePath, item.url) {
			stored[item.url] = item.url
			continue
		}
		data, err := readVaultImage(ctx, item.url, read)
		if err != nil {
			return "", fmt.Errorf("copy vault image %s: %w", item.url, err)
		}
		mediaType := http.DetectContentType(data)
		if !strings.HasPrefix(mediaType, "image/") {
			return "", fmt.Errorf("vault attachment is not an image: %s", item.url)
		}
		ext := vaultImageExtension(item.url, mediaType)
		hash := sha256.Sum256(data)
		attachment := filepath.ToSlash(filepath.Join("attachments", k.ID, fmt.Sprintf("image-%x%s", hash[:8], ext)))
		if err = v.writeAttachment(attachment, data); err != nil {
			return "", err
		}
		relative, err := filepath.Rel(filepath.Dir(filepath.FromSlash(articlePath)), filepath.FromSlash(attachment))
		if err != nil {
			return "", err
		}
		stored[item.url] = filepath.ToSlash(relative)
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, item := range replacements {
		body = body[:item.start] + stored[item.url] + body[item.end:]
	}
	return body, nil
}

func readVaultImage(ctx context.Context, ref string, read func(context.Context, string) (io.ReadCloser, error)) ([]byte, error) {
	if strings.HasPrefix(ref, "data:") {
		comma := strings.IndexByte(ref, ',')
		if comma < 0 {
			return nil, errors.New("invalid data URI")
		}
		if strings.HasSuffix(ref[:comma], ";base64") {
			return base64.StdEncoding.DecodeString(ref[comma+1:])
		}
		value, err := url.PathUnescape(ref[comma+1:])
		return []byte(value), err
	}
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return nil, errors.New("remote images must be provided through the ingest interface")
	}
	if read == nil {
		return nil, errors.New("image reader is unavailable")
	}
	file, err := read(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func vaultImageExtension(ref, mediaType string) string {
	ext := strings.ToLower(filepath.Ext(strings.SplitN(ref, "?", 2)[0]))
	for _, allowed := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif"} {
		if ext == allowed {
			return ext
		}
	}
	if extensions, _ := mime.ExtensionsByType(mediaType); len(extensions) > 0 {
		sort.Strings(extensions)
		return extensions[0]
	}
	return ".img"
}

// RefreshHeader reads fields from PostgreSQL and replaces only the leading
// front matter. Missing files are rebuilt from the saved body in PostgreSQL.
func (v *VaultService) RefreshHeader(ctx context.Context, entryID string) (refreshErr error) {
	defer func() {
		if refreshErr != nil {
			v.recordArticleError(ctx, entryID, refreshErr)
		}
	}()
	lock := v.articleLock(entryID)
	lock.Lock()
	defer lock.Unlock()
	var k types.Entry
	query := v.db.WithContext(ctx).Where("id=?", entryID)
	if space, ok := types.SpaceIDFromContext(ctx); ok {
		query = query.Where("space_id=?", space)
	}
	if err := query.First(&k).Error; err != nil {
		return err
	}
	if (k.SourceKind != "report" && !isIngestArticle(&k)) || k.VaultPath == "" || k.ParseStatus == types.ParseStatusDeleting {
		return nil
	}
	directory, err := vaultLibraryDirectory(v.db.WithContext(ctx), k.LibraryID)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.ToSlash(filepath.Clean(k.VaultPath)), directory+"/") {
		return errors.New("entry path must belong to its library directory")
	}
	abs, err := vaultAbsolute(v.root, k.VaultPath)
	if err != nil {
		return err
	}
	h, err := v.articleHeader(ctx, &k)
	if err != nil {
		return err
	}
	header := RenderVaultArticleHeader(h)
	current, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		body := []byte(k.VaultBody)
		if err = v.atomicArticleWrite(abs, header, body, nil); err != nil {
			return err
		}
		logger.Warnf(ctx, "Rebuilt externally deleted article %s from PostgreSQL at %s", k.ID, k.VaultPath)
		return nil
	}
	if err != nil {
		return err
	}
	oldHeader, body, err := SplitVaultArticle(current)
	if err != nil || !bytes.Equal(body, []byte(k.VaultBody)) {
		return v.atomicArticleWrite(abs, header, []byte(k.VaultBody), nil)
	}
	if bytes.Equal(header, oldHeader) {
		return nil
	}
	return v.atomicArticleWrite(abs, header, body, current)
}

// atomicArticleWrite verifies the actual temporary file's body before rename.
// A mismatch leaves the destination untouched and removes the temporary file.
func (v *VaultService) atomicArticleWrite(abs string, header, body, previous []byte) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	data := append(bytes.Clone(header), body...)
	if hook := v.articles.beforeTempWrite; hook != nil {
		data = hook(data)
	}
	file, err := os.CreateTemp(filepath.Dir(abs), ".vault-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0644); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	actual, err := os.ReadFile(file.Name())
	if err != nil {
		return err
	}
	_, actualBody, err := SplitVaultArticle(actual)
	if err != nil || sha256.Sum256(actualBody) != sha256.Sum256(body) {
		return errors.New("vault body hash changed during header rewrite; replacement refused")
	}
	if previous != nil {
		current, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		_, currentBody, err := SplitVaultArticle(current)
		_, previousBody, previousErr := SplitVaultArticle(previous)
		if err != nil || previousErr != nil || sha256.Sum256(currentBody) != sha256.Sum256(previousBody) {
			return errors.New("vault body changed concurrently; replacement refused")
		}
	}
	if err = os.Rename(file.Name(), abs); err != nil {
		return err
	}
	v.articles.mu.Lock()
	v.articles.writeCount++
	v.articles.mu.Unlock()
	return nil
}

func (v *VaultService) recordArticleError(ctx context.Context, id string, err error) {
	v.articles.mu.Lock()
	v.articles.recentError = fmt.Sprintf("%s: %v", id, err)
	v.articles.mu.Unlock()
	logger.Errorf(ctx, "Vault article %s: %v", id, err)
}

func (v *VaultService) ArticleError() string {
	v.articles.mu.Lock()
	defer v.articles.mu.Unlock()
	return v.articles.recentError
}

// ScheduleRefresh coalesces repeated changes per article using a trailing
// timer. The context keeps space identity but outlives its HTTP request.
func (v *VaultService) ScheduleRefresh(ctx context.Context, entryID string) {
	v.articles.mu.Lock()
	defer v.articles.mu.Unlock()
	if v.articles.stopped {
		return
	}
	if v.articles.timers == nil {
		v.articles.timers = make(map[string]*time.Timer)
	}
	if prior := v.articles.timers[entryID]; prior != nil && prior.Stop() {
		v.articles.wg.Done()
	}
	delay := v.cfg.HeaderDebounce
	if delay <= 0 {
		delay = 5 * time.Second
	}
	ctx = context.WithoutCancel(ctx)
	v.articles.wg.Add(1)
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		defer v.articles.wg.Done()
		v.articles.mu.Lock()
		if v.articles.timers[entryID] == timer {
			delete(v.articles.timers, entryID)
		}
		v.articles.mu.Unlock()
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_ = v.RefreshHeader(refreshCtx, entryID)
	})
	v.articles.timers[entryID] = timer
}

func (v *VaultService) Close() error {
	v.articles.mu.Lock()
	v.articles.stopped = true
	for id, timer := range v.articles.timers {
		if timer.Stop() {
			v.articles.wg.Done()
		}
		delete(v.articles.timers, id)
	}
	v.articles.mu.Unlock()
	v.articles.wg.Wait()
	if err := v.Flush(context.Background()); err != nil {
		return err
	}
	if v.replica != nil {
		return v.replica.Close()
	}
	return nil
}

func (v *VaultService) ReadBody(k *types.Entry) ([]byte, error) {
	abs, err := vaultAbsolute(v.root, k.VaultPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if !isIngestArticle(k) && k.SourceKind != "report" {
		return data, nil
	}
	_, body, err := SplitVaultArticle(data)
	return body, err
}

func (v *VaultService) ReadImage(k *types.Entry, ref string) (io.ReadCloser, error) {
	relative := filepath.Join(filepath.Dir(filepath.FromSlash(k.VaultPath)), filepath.FromSlash(ref))
	abs, err := vaultAbsolute(v.root, filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	return os.Open(abs)
}

// DeleteArticle removes imported article or user document files and attachments.
func (v *VaultService) DeleteArticle(ctx context.Context, k *types.Entry) error {
	if !isIngestArticle(k) && k.SourceKind != "report" {
		return nil
	}
	if _, err := uuid.Parse(k.ID); err != nil {
		return fmt.Errorf("article id must be a UUID: %w", err)
	}
	lock := v.articleLock(k.ID)
	lock.Lock()
	defer lock.Unlock()
	v.articles.mu.Lock()
	if timer := v.articles.timers[k.ID]; timer != nil {
		if timer.Stop() {
			v.articles.wg.Done()
		}
		delete(v.articles.timers, k.ID)
	}
	v.articles.mu.Unlock()
	if k.VaultPath != "" {
		directory, err := vaultLibraryDirectory(v.db.WithContext(ctx), k.LibraryID)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(k.VaultPath, directory+"/") {
			return errors.New("entry file must belong to its library directory")
		}
		abs, err := vaultAbsolute(v.root, k.VaultPath)
		if err != nil {
			return err
		}
		if err = os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	abs, err := vaultAbsolute(v.root, filepath.ToSlash(filepath.Join("attachments", k.ID)))
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}
