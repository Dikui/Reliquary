package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/logger"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// VaultDirectoryStatus describes indexing of one directory, including the last scan failure.
type VaultDirectoryStatus struct {
	LastScanAt   *time.Time `json:"last_scan_at"`
	IndexedFiles int64      `json:"indexed_files"`
	LastError    string     `json:"last_error"`
}

type VaultStatus struct {
	Root    string               `json:"root"`
	Mine    VaultDirectoryStatus `json:"mine"`
	Reports VaultDirectoryStatus `json:"reports"`
}

type vaultObservation struct {
	hash      string
	changedAt time.Time
}

// VaultDocumentService reads personal documents and reports without article processing.
// File contents are never modified by indexing or rebuilding.
type VaultDocumentService struct {
	db           *gorm.DB
	models       interfaces.ModelService
	retrieval    interfaces.RetrievalService
	root         string
	stableWait   time.Duration
	scanInterval time.Duration
	mu           sync.Mutex
	observations map[string]vaultObservation
	statusMu     sync.RWMutex
	status       VaultStatus
	lifecycleMu  sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	watcher      *fsnotify.Watcher
}

func NewVaultDocumentService(db *gorm.DB, models interfaces.ModelService, retrieval interfaces.RetrievalService, root string, wait, scan time.Duration) (*VaultDocumentService, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("VAULT_ROOT is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, directory := range []string{"articles", "mine", "reports", "attachments"} {
		path, err := vaultAbsolute(root, directory)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("create vault directory %s: %w", directory, err)
		}
	}
	if wait <= 0 {
		wait = 10 * time.Minute
	}
	if scan <= 0 {
		scan = time.Minute
	}
	return &VaultDocumentService{db: db, models: models, retrieval: retrieval, root: root,
		stableWait: wait, scanInterval: scan, observations: make(map[string]vaultObservation),
		status: VaultStatus{Root: root}}, nil
}

func (s *VaultDocumentService) scope(ctx context.Context) (context.Context, *types.KnowledgeBase, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		tenantID = types.SingleTenantID
		ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	}
	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).First(&kb).Error; err != nil {
		return ctx, nil, fmt.Errorf("vault indexing requires an initialized knowledge base: %w", err)
	}
	return ctx, &kb, nil
}

// Start watches both trees and schedules full scans to recover missed events.
func (s *VaultDocumentService) Start(ctx context.Context) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.cancel != nil {
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, name := range []string{"mine", "reports"} {
		if err := s.watchTree(watcher, filepath.Join(s.root, name)); err != nil {
			_ = watcher.Close()
			return err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel, s.watcher, s.done = cancel, watcher, make(chan struct{})
	go s.watch(ctx, watcher, s.done)
	return nil
}

func (s *VaultDocumentService) watchTree(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
}

func (s *VaultDocumentService) watch(ctx context.Context, watcher *fsnotify.Watcher, done chan struct{}) {
	defer close(done)
	defer watcher.Close()
	// A short scheduling tick handles files becoming stable between fallback scans.
	tick := s.stableWait / 4
	if tick > time.Second {
		tick = time.Second
	}
	if tick < 10*time.Millisecond {
		tick = 10 * time.Millisecond
	}
	settled := time.NewTicker(tick)
	fallback := time.NewTicker(s.scanInterval)
	defer settled.Stop()
	defer fallback.Stop()
	nextDue := time.Now().Add(s.stableWait)
	s.runScan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := s.watchTree(watcher, event.Name); err != nil {
						s.recordWatchError(event.Name, err)
					}
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				rel, err := filepath.Rel(s.root, event.Name)
				if err == nil && strings.EqualFold(filepath.Ext(rel), ".md") {
					s.mu.Lock()
					observation := s.observations[filepath.ToSlash(rel)]
					observation.changedAt = time.Now()
					s.observations[filepath.ToSlash(rel)] = observation
					s.mu.Unlock()
				}
				// Scan now to remove deleted files, then scan when the writer settles.
				s.runScan(ctx)
				nextDue = time.Now().Add(s.stableWait)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.recordWatchError("", err)
		case <-settled.C:
			if !nextDue.IsZero() && !time.Now().Before(nextDue) {
				s.runScan(ctx)
				nextDue = time.Time{}
			}
		case <-fallback.C:
			s.runScan(ctx)
			nextDue = time.Now().Add(s.stableWait)
		}
	}
}

func (s *VaultDocumentService) runScan(ctx context.Context) {
	paused, err := maintenanceops.SnapshotPaused(ctx, s.db)
	if err != nil {
		if ctx.Err() == nil {
			logger.Errorf(ctx, "Vault maintenance status unavailable: %v", err)
		}
		return
	}
	if paused {
		return
	}
	if err := s.Scan(ctx, false); err != nil && ctx.Err() == nil {
		logger.Errorf(ctx, "Vault document scan failed: %v", err)
	}
}

func (s *VaultDocumentService) recordWatchError(path string, err error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if strings.HasPrefix(path, filepath.Join(s.root, "mine")) {
		s.status.Mine.LastError = err.Error()
	} else if strings.HasPrefix(path, filepath.Join(s.root, "reports")) {
		s.status.Reports.LastError = err.Error()
	} else {
		s.status.Mine.LastError, s.status.Reports.LastError = err.Error(), err.Error()
	}
}

func (s *VaultDocumentService) Close() error {
	s.lifecycleMu.Lock()
	if s.cancel == nil {
		s.lifecycleMu.Unlock()
		return nil
	}
	cancel, done := s.cancel, s.done
	s.cancel = nil
	cancel()
	s.lifecycleMu.Unlock()
	<-done
	return nil
}

// Scan waits for stable contents unless force is set, as during a read-only rebuild.
func (s *VaultDocumentService) Scan(ctx context.Context, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, kb, err := s.scope(ctx)
	if err != nil {
		s.recordWatchError("", err)
		return err
	}
	var allErrors []error
	for _, kind := range []string{"mine", "report"} {
		if err := s.scanDirectory(ctx, kb, kind, force); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return errors.Join(allErrors...)
}

func vaultDirectory(kind string) string {
	if kind == "report" {
		return "reports"
	}
	return "mine"
}

func (s *VaultDocumentService) scanDirectory(ctx context.Context, kb *types.KnowledgeBase, kind string, force bool, skipped ...*[]VaultRebuildSkippedFile) error {
	directory := vaultDirectory(kind)
	seen := make(map[string]bool)
	var scanErrors []error
	recordFileError := func(path string, err error) {
		if len(skipped) > 0 {
			*skipped[0] = append(*skipped[0], VaultRebuildSkippedFile{path, err.Error()})
		} else {
			scanErrors = append(scanErrors, fmt.Errorf("%s: %w", path, err))
		}
	}
	now := time.Now().UTC()
	directoryPath, directoryErr := vaultAbsolute(s.root, directory)
	if directoryErr != nil {
		s.recordWatchError(filepath.Join(s.root, directory), directoryErr)
		return directoryErr
	}
	walkErr := filepath.WalkDir(directoryPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Symlinks are not followed, so indexing never leaves the local vault.
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		body, err := os.ReadFile(path)
		if err != nil {
			recordFileError(rel, err)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		hash := vaultContentHash(string(body))
		observation, observed := s.observations[rel]
		if !observed {
			observation.changedAt = info.ModTime()
		} else if observation.hash != "" && observation.hash != hash {
			observation.changedAt = now
		}
		observation.hash = hash
		s.observations[rel] = observation
		if !force && now.Sub(observation.changedAt) < s.stableWait {
			return nil
		}
		if !utf8.Valid(body) {
			recordFileError(rel, fmt.Errorf("Markdown must be valid UTF-8"))
			return nil
		}
		if _, err := s.indexDocument(ctx, kb, rel, kind, string(body), info.ModTime()); err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("%s: %w", rel, err))
		}
		return nil
	})
	if walkErr != nil {
		scanErrors = append(scanErrors, walkErr)
	} else {
		// Only remove missing records after a complete traversal of the directory.
		var existing []types.Knowledge
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND source_kind = ?", kb.TenantID, kind).Find(&existing).Error; err != nil {
			scanErrors = append(scanErrors, err)
		} else {
			for _, knowledge := range existing {
				if !seen[knowledge.VaultPath] {
					if err := s.deleteDocument(ctx, &knowledge); err != nil {
						scanErrors = append(scanErrors, err)
					}
					delete(s.observations, knowledge.VaultPath)
				}
			}
		}
	}
	s.statusMu.Lock()
	status := &s.status.Mine
	if kind == "report" {
		status = &s.status.Reports
	}
	status.LastScanAt = &now
	if len(scanErrors) > 0 {
		status.LastError = errors.Join(scanErrors...).Error()
	} else {
		status.LastError = ""
	}
	s.statusMu.Unlock()
	return errors.Join(scanErrors...)
}

func vaultContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func vaultDocumentTitle(path, body, kind string) string {
	// Fenced examples and optional frontmatter do not supply document headings.
	text := body
	if _, content, err := splitVaultHeader([]byte(body)); err == nil {
		text = string(content)
	}
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence == "" && strings.HasPrefix(line, "# ") && strings.TrimSpace(line[2:]) != "" {
			return vaultTrimTitle(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[2:]), " #")))
		}
	}
	return vaultTrimTitle(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
}

func vaultTrimTitle(title string) string {
	runes := []rune(title)
	if len(runes) > 255 {
		return string(runes[:255])
	}
	return title
}

func (s *VaultDocumentService) indexDocument(ctx context.Context, kb *types.KnowledgeBase, path, kind, body string, modified time.Time) (*types.Knowledge, error) {
	var knowledge types.Knowledge
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND vault_path = ?", kb.TenantID, path).First(&knowledge).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash := vaultContentHash(body)
	if err == nil && knowledge.SourceKind != kind {
		return nil, fmt.Errorf("vault path already belongs to a different source kind")
	}
	if err == nil && knowledge.FileHash == hash && knowledge.ParseStatus == types.ParseStatusCompleted {
		return &knowledge, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		knowledge = types.Knowledge{ID: uuid.NewString(), TenantID: kb.TenantID, KnowledgeBaseID: kb.ID,
			Type: types.KnowledgeTypeManual, SourceKind: kind, VaultPath: path, Source: path,
			FileName: filepath.Base(path), FileType: "md", EnableStatus: "enabled", Channel: types.ChannelAPI,
			CreatedAt: modified.UTC(), ProcessingMode: types.ProcessingModeFull}
		if err := s.db.WithContext(ctx).Create(&knowledge).Error; err != nil {
			return nil, err
		}
	}
	knowledge.Title = vaultDocumentTitle(path, body, kind)
	// User documents and reports have a single original in the file library.
	// Their text is used for indexing, never stored as a recovery copy in SQL.
	knowledge.VaultBody = ""
	knowledge.FileSize = int64(len(body))
	knowledge.FileHash = hash
	knowledge.UpdatedAt = time.Now().UTC()
	indexBody := body
	if kind == "report" {
		if header, content, err := splitVaultHeader([]byte(body)); err == nil {
			var report struct {
				RelatedArticleIDs []string `yaml:"related_article_ids"`
			}
			if yaml.Unmarshal(header, &report) == nil {
				metadata, _ := json.Marshal(map[string]interface{}{"related_article_ids": report.RelatedArticleIDs})
				knowledge.Metadata = types.JSON(metadata)
				indexBody = string(content)
			}
		}
	}
	if err := s.indexVaultChunks(ctx, kb, &knowledge, indexBody); err != nil {
		_ = s.db.WithContext(ctx).Model(&types.Knowledge{}).Where("id=? AND tenant_id=?", knowledge.ID, kb.TenantID).
			Updates(map[string]interface{}{"parse_status": types.ParseStatusFailed, "error_message": err.Error()}).Error
		return nil, err
	}
	now := time.Now().UTC()
	knowledge.ParseStatus, knowledge.ErrorMessage = types.ParseStatusCompleted, ""
	knowledge.ProcessedAt = &now
	if err := s.db.WithContext(ctx).Save(&knowledge).Error; err != nil {
		return nil, err
	}
	return &knowledge, nil
}

// indexVaultChunks matches content hashes across positions, keeping unchanged vectors.
func (s *VaultDocumentService) indexVaultChunks(ctx context.Context, kb *types.KnowledgeBase, knowledge *types.Knowledge, body string) error {
	var embedder embedding.Embedder
	if kb.IndexingStrategy.VectorEnabled {
		var err error
		embedder, err = GetTierEmbeddingModel(ctx, s.db, s.models, kb)
		if err != nil {
			return err
		}
		knowledge.EmbeddingModelID = embedder.GetModelID()
	}
	var previous []*types.Chunk
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND knowledge_id=?", knowledge.TenantID, knowledge.ID).Order("chunk_index, id").Find(&previous).Error; err != nil {
		return err
	}
	byHash := make(map[string][]*types.Chunk)
	for _, chunk := range previous {
		byHash[chunk.ContentHash] = append(byHash[chunk.ContentHash], chunk)
	}
	parts := chunker.Split(chunker.NormalizeLineEndings(body), buildSplitterConfig(kb))
	current := make([]*types.Chunk, 0, len(parts))
	added := make([]*types.Chunk, 0, len(parts))
	used := make(map[string]bool)
	now := time.Now().UTC()
	for _, part := range parts {
		if strings.TrimSpace(part.Content) == "" {
			continue
		}
		// The prefix affects the vector even when the body is unchanged.
		hash := vaultContentHash(buildKnowledgeIndexContent(knowledge, part.Content))
		var chunk *types.Chunk
		if matches := byHash[hash]; len(matches) > 0 {
			chunk = matches[0]
			byHash[hash] = matches[1:]
			used[chunk.ID] = true
		} else {
			chunk = &types.Chunk{ID: uuid.NewString(), TenantID: knowledge.TenantID, KnowledgeID: knowledge.ID,
				KnowledgeBaseID: kb.ID, IsEnabled: true, ChunkType: types.ChunkTypeText,
				ContentHash: hash, CreatedAt: now}
			added = append(added, chunk)
		}
		chunk.Content, chunk.ContextHeader = part.Content, part.ContextHeader
		chunk.ChunkIndex, chunk.StartAt, chunk.EndAt = len(current), part.Start, part.End
		chunk.PreChunkID, chunk.NextChunkID = "", ""
		current = append(current, chunk)
	}
	for i, chunk := range current {
		if i > 0 {
			chunk.PreChunkID = current[i-1].ID
		}
		if i+1 < len(current) {
			chunk.NextChunkID = current[i+1].ID
		}
	}
	removed := make([]string, 0)
	for _, chunk := range previous {
		if !used[chunk.ID] {
			removed = append(removed, chunk.ID)
		}
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, chunk := range current {
			if err := tx.Save(chunk).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if embedder != nil && len(added) > 0 {
		indices := make([]*types.IndexInfo, 0, len(added))
		for _, chunk := range added {
			indices = append(indices, &types.IndexInfo{Content: buildKnowledgeIndexContent(knowledge, chunk.Content), SourceID: chunk.ID,
				SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: knowledge.ID,
				KnowledgeBaseID: kb.ID, IsEnabled: true})
		}
		if err := s.retrieval.BatchIndex(ctx, embedder, indices); err != nil {
			// Failed new chunks must not be reused as if their vectors were complete.
			ids := make([]string, 0, len(added))
			for _, chunk := range added {
				ids = append(ids, chunk.ID)
			}
			_ = s.retrieval.DeleteByChunkIDList(ctx, ids, 0, knowledge.Type)
			_ = s.db.WithContext(ctx).Unscoped().Where("tenant_id=? AND id IN ?", knowledge.TenantID, ids).Delete(&types.Chunk{}).Error
			return err
		}
	}
	if len(removed) > 0 {
		if s.retrieval != nil {
			if err := s.retrieval.DeleteByChunkIDList(ctx, removed, 0, knowledge.Type); err != nil {
				return err
			}
		}
		if err := s.db.WithContext(ctx).Unscoped().Where("tenant_id=? AND id IN ?", knowledge.TenantID, removed).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *VaultDocumentService) deleteDocument(ctx context.Context, knowledge *types.Knowledge) error {
	if s.retrieval != nil {
		if err := s.retrieval.DeleteByKnowledgeIDList(ctx, []string{knowledge.ID}, 0, knowledge.Type); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("tenant_id=? AND knowledge_id=?", knowledge.TenantID, knowledge.ID).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("tenant_id=? AND id=?", knowledge.TenantID, knowledge.ID).Delete(&types.Knowledge{}).Error
	})
}

func (s *VaultDocumentService) Status(ctx context.Context) (VaultStatus, error) {
	s.statusMu.RLock()
	result := s.status
	s.statusMu.RUnlock()
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		tenantID = types.SingleTenantID
	}
	for _, target := range []struct {
		kind  string
		count *int64
	}{{"mine", &result.Mine.IndexedFiles}, {"report", &result.Reports.IndexedFiles}} {
		if err := s.db.WithContext(ctx).Model(&types.Knowledge{}).
			Where("tenant_id=? AND source_kind=? AND parse_status=?", tenantID, target.kind, types.ParseStatusCompleted).
			Count(target.count).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}

// CreateReport is shared by the REST endpoint and future external-agent interfaces.
func (s *VaultDocumentService) CreateReport(ctx context.Context, title, markdown string, relatedIDs []string) (*types.Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 255 || !utf8.ValidString(title) || strings.TrimSpace(markdown) == "" || !utf8.ValidString(markdown) {
		return nil, werrors.NewBadRequestError("report requires a title of up to 255 characters and valid Markdown")
	}
	ctx, kb, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	selected := []string{}
	if len(relatedIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&types.Knowledge{}).
			Where("tenant_id=? AND source_kind='article' AND id IN ?", kb.TenantID, relatedIDs).
			Order("id").Pluck("id", &selected).Error; err != nil {
			return nil, err
		}
		unique := make(map[string]bool)
		for _, id := range relatedIDs {
			unique[id] = true
		}
		if len(selected) != len(unique) {
			return nil, werrors.NewBadRequestError("related_article_ids contains an unavailable article")
		}
	}
	created := time.Now().UTC()
	id := uuid.NewString()
	path := filepath.ToSlash(filepath.Join("reports", created.Format("2006-01"), vaultSlug(title)+"-"+id[:8]+".md"))
	header := struct {
		Title             string   `yaml:"title"`
		CreatedAt         string   `yaml:"created_at"`
		RelatedArticleIDs []string `yaml:"related_article_ids"`
	}{title, created.Format(time.RFC3339Nano), selected}
	raw, err := yaml.Marshal(header)
	if err != nil {
		return nil, err
	}
	content := "---\n" + string(raw) + "---\n" + markdown
	absolutePath, err := vaultAbsolute(s.root, path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(absolutePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.WriteString(content)
	if syncErr := file.Sync(); writeErr == nil {
		writeErr = syncErr
	}
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(absolutePath)
		return nil, writeErr
	}
	s.observations[path] = vaultObservation{hash: vaultContentHash(content), changedAt: created.Add(-s.stableWait)}
	var knowledge *types.Knowledge
	indexed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactional := &VaultDocumentService{db: tx, models: s.models,
			retrieval: retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(tx))}
		knowledge, err = transactional.indexDocument(ctx, kb, path, "report", content, created)
		if err != nil {
			return err
		}
		indexed = true
		return NewOperationLogService(tx).RecordTx(ctx, tx, "write_report", knowledge.ID, nil,
			map[string]any{"title": title, "vault_path": path, "related_article_ids": selected})
	})
	if err != nil {
		if indexed {
			delete(s.observations, path)
			if cleanupErr := os.Remove(absolutePath); cleanupErr != nil {
				return nil, fmt.Errorf("report transaction failed: %v; remove unsaved report: %w", err, cleanupErr)
			}
			return nil, err
		}
		// Keep the saved source on ordinary indexing failure, as other vault
		// documents do, so a later scan can retry the same immutable file.
		return nil, fmt.Errorf("report saved at %s but indexing failed: %w", path, err)
	}
	return knowledge, nil
}

// splitVaultHeader recognizes only a frontmatter block at byte zero.
func splitVaultHeader(content []byte) ([]byte, []byte, error) {
	text := string(content)
	first := strings.IndexByte(text, '\n')
	if first < 0 || strings.TrimSuffix(text[:first], "\r") != "---" {
		return nil, content, fmt.Errorf("missing YAML frontmatter")
	}
	start := first + 1
	for cursor := start; cursor < len(text); {
		end := strings.IndexByte(text[cursor:], '\n')
		lineEnd := len(text)
		if end >= 0 {
			lineEnd = cursor + end
		}
		if strings.TrimSuffix(text[cursor:lineEnd], "\r") == "---" {
			bodyStart := lineEnd
			if bodyStart < len(text) {
				bodyStart++
			}
			return content[start:cursor], content[bodyStart:], nil
		}
		if end < 0 {
			break
		}
		cursor = lineEnd + 1
	}
	return nil, content, fmt.Errorf("unterminated YAML frontmatter")
}

// sortedUniqueNames supplies a stable restoration order for metadata references.
func sortedUniqueNames(names []string) []string {
	seen := make(map[string]bool)
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			seen[name] = true
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
