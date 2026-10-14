package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/logger"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/vaultpath"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"gorm.io/gorm"
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

// VaultDocumentService indexes reports and rebuilds imported documents.
// Only reports are watched for file changes.
type VaultDocumentService struct {
	db                 *gorm.DB
	models             interfaces.ModelService
	retrieval          interfaces.RetrievalService
	root               string
	reportStableWait   time.Duration
	reportScanInterval time.Duration
	mu                 sync.Mutex
	observations       map[string]vaultObservation
	statusMu           sync.RWMutex
	status             VaultStatus
	lifecycleMu        sync.Mutex
	cancel             context.CancelFunc
	done               chan struct{}
	watcher            *fsnotify.Watcher
}

func NewVaultDocumentService(db *gorm.DB, models interfaces.ModelService, retrieval interfaces.RetrievalService, root string) (*VaultDocumentService, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("VAULT_ROOT is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := ensureVaultDirectories(db, root); err != nil {
		return nil, err
	}
	return &VaultDocumentService{db: db, models: models, retrieval: retrieval, root: root,
		reportStableWait: 10 * time.Minute, reportScanInterval: time.Minute, observations: make(map[string]vaultObservation),
		status: VaultStatus{Root: root}}, nil
}

func (s *VaultDocumentService) scope(ctx context.Context) (context.Context, *types.Library, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok || spaceID == 0 {
		spaceID = types.SingleSpaceID
		ctx = context.WithValue(ctx, types.SpaceIDContextKey, spaceID)
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("space_id = ? AND system_role = ?", spaceID, "ai-reports").First(&library).Error; err != nil {
		return ctx, nil, fmt.Errorf("vault indexing requires an initialized library: %w", err)
	}
	return ctx, &library, nil
}

// Start watches reports and schedules scans to recover missed report events.
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

	var libraries []types.Library
	if err := s.db.WithContext(ctx).Where("template=?", types.TemplateGenerated).Find(&libraries).Error; err != nil {
		_ = watcher.Close()
		return err
	}
	for _, library := range libraries {
		if err := s.watchTree(watcher, filepath.Join(s.root, library.Directory)); err != nil {
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
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		if vaultpath.UserMetadata(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
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
	tick := s.reportStableWait / 4
	if tick > time.Second {
		tick = time.Second
	}
	if tick < 10*time.Millisecond {
		tick = 10 * time.Millisecond
	}
	settled := time.NewTicker(tick)
	fallback := time.NewTicker(s.reportScanInterval)
	defer settled.Stop()
	defer fallback.Stop()
	nextDue := time.Now().Add(s.reportStableWait)
	s.runScan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			rel, err := filepath.Rel(s.root, event.Name)
			if err == nil && vaultpath.UserMetadata(rel) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := s.watchTree(watcher, event.Name); err != nil {
						s.recordReportWatchError(ctx, err)
					}
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				if err == nil && strings.EqualFold(filepath.Ext(rel), ".md") {
					s.mu.Lock()
					observation := s.observations[filepath.ToSlash(rel)]
					observation.changedAt = time.Now()
					s.observations[filepath.ToSlash(rel)] = observation
					s.mu.Unlock()
				}
				// Scan now to remove deleted files, then scan when the writer settles.
				s.runScan(ctx)
				nextDue = time.Now().Add(s.reportStableWait)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.recordReportWatchError(ctx, err)
		case <-settled.C:
			if !nextDue.IsZero() && !time.Now().Before(nextDue) {
				s.runScan(ctx)
				nextDue = time.Time{}
			}
		case <-fallback.C:
			s.runScan(ctx)
			nextDue = time.Now().Add(s.reportStableWait)
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

func (s *VaultDocumentService) recordReportWatchError(ctx context.Context, err error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	s.status.Reports.LastError = err.Error()
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
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, library, err := s.scope(ctx)
	if err != nil {
		s.recordReportWatchError(ctx, err)
		return err
	}
	return s.scanReports(ctx, library, force)
}

func (s *VaultDocumentService) scanReports(ctx context.Context, library *types.Library, force bool, skipped ...*[]VaultRebuildSkippedFile) error {
	var reports []types.Entry
	if err := s.db.WithContext(ctx).Where("space_id=? AND source_kind='report' AND vault_path<>''", library.SpaceID).Find(&reports).Error; err != nil {
		return err
	}
	vault := &VaultService{db: s.db, root: s.root}
	for _, report := range reports {
		if err := vault.RefreshHeader(ctx, report.ID); err != nil {
			return err
		}
		if report.ParseStatus != types.ParseStatusCompleted {
			if !force {
				managed, err := s.reportIndexManagedByQueue(ctx, report.SpaceID, report.VaultPath)
				if err != nil {
					return err
				}
				if managed {
					continue
				}
			}
			var actual types.Library
			if err := s.db.WithContext(ctx).First(&actual, "id=?", report.LibraryID).Error; err != nil {
				return err
			}
			if _, err := s.retryReportIndex(ctx, &actual, &report); err != nil {
				return err
			}
		}
	}
	now := time.Now().UTC()
	s.statusMu.Lock()
	s.status.Reports.LastScanAt = &now
	s.status.Reports.IndexedFiles = int64(len(reports))
	s.status.Reports.LastError = ""
	s.statusMu.Unlock()
	return nil
}

func vaultContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func vaultDocumentTitle(path, body string) string {
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

func (s *VaultDocumentService) indexReport(ctx context.Context, library *types.Library, path, body string, modified time.Time) (*types.Entry, error) {
	var entry types.Entry
	err := s.db.WithContext(ctx).Where("space_id = ? AND vault_path = ?", library.SpaceID, path).First(&entry).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("report is not registered in the database")
	}
	if entry.SourceKind != "report" {
		return nil, fmt.Errorf("vault path already belongs to a different source kind")
	}
	hash := vaultContentHash(body)
	if entry.FileHash == hash && entry.ParseStatus == types.ParseStatusCompleted {
		return &entry, nil
	}
	entry.FileSize = int64(len(body))
	entry.FileHash = hash
	entry.UpdatedAt = time.Now().UTC()
	indexBody := entry.VaultBody
	if err := s.indexVaultChunks(ctx, library, &entry, indexBody); err != nil {
		_ = s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, library.SpaceID).
			Updates(map[string]interface{}{"parse_status": types.ParseStatusFailed, "error_message": err.Error()}).Error
		return nil, err
	}
	now := time.Now().UTC()
	entry.ParseStatus, entry.ErrorMessage = types.ParseStatusCompleted, ""
	entry.ProcessedAt = &now
	if err := s.db.WithContext(ctx).Save(&entry).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

// indexVaultChunks matches content hashes across positions, keeping unchanged vectors.
func (s *VaultDocumentService) indexVaultChunks(ctx context.Context, library *types.Library, entry *types.Entry, body string) error {
	ctx = types.WithLLMCallMetadata(types.WithModelCallEntry(ctx, entry.ID), string(types.CallPurposeBodyEmbedding), "")
	var embedder embedding.Embedder
	if library.IndexingStrategy.VectorEnabled {
		var err error
		embedder, err = GetTierEmbeddingModel(ctx, s.db, s.models, library)
		if err != nil {
			return err
		}
		entry.EmbeddingModelID = embedder.GetModelID()
	}
	var previous []*types.Chunk
	if err := s.db.WithContext(ctx).Where("space_id=? AND entry_id=?", entry.SpaceID, entry.ID).Order("chunk_index, id").Find(&previous).Error; err != nil {
		return err
	}
	byHash := make(map[string][]*types.Chunk)
	for _, chunk := range previous {
		if chunk.ChunkType != types.ChunkTypeText {
			continue
		}
		byHash[chunk.ContentHash] = append(byHash[chunk.ContentHash], chunk)
	}
	parts := chunker.Split(chunker.NormalizeLineEndings(body), buildSplitterConfig(library))
	current := make([]*types.Chunk, 0, len(parts))
	added := make([]*types.Chunk, 0, len(parts))
	used := make(map[string]bool)
	now := time.Now().UTC()
	for _, part := range parts {
		if strings.TrimSpace(part.Content) == "" {
			continue
		}
		// The prefix affects the vector even when the body is unchanged.
		hash := vaultContentHash(buildEntryIndexContent(entry, part.Content))
		var chunk *types.Chunk
		if matches := byHash[hash]; len(matches) > 0 {
			chunk = matches[0]
			byHash[hash] = matches[1:]
			used[chunk.ID] = true
		} else {
			chunk = &types.Chunk{ID: uuid.NewString(), SpaceID: entry.SpaceID, EntryID: entry.ID,
				LibraryID: library.ID, IsEnabled: true, ChunkType: types.ChunkTypeText,
				ContentHash: hash, CreatedAt: now}
			added = append(added, chunk)
		}
		chunk.Content, chunk.ContextHeader = part.Content, part.ContextHeader
		chunk.ChunkIndex, chunk.StartAt, chunk.EndAt = len(current), part.Start, part.End
		chunk.PreChunkID, chunk.NextChunkID = "", ""
		types.AssignStoredBodyLocations(body, chunk)
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
	output, err := ReadEntryProcessingOutput(ctx, s.db, entry.SpaceID, entry.ID)
	if err != nil {
		return err
	}
	if output != nil && output.BodyHash == fmt.Sprintf("%x", sha256.Sum256([]byte(body))) {
		for _, caption := range imageDescriptionChunks(entry, output.Images, len(current)) {
			caption.ContentHash = vaultContentHash(buildEntryIndexContent(entry, caption.Content))
			reused := false
			for _, old := range previous {
				if !used[old.ID] && old.ChunkType == types.ChunkTypeImageDescription && old.ContentHash == caption.ContentHash {
					caption.ID, caption.CreatedAt = old.ID, old.CreatedAt
					used[old.ID] = true
					reused = true
					break
				}
			}
			current = append(current, caption)
			if !reused {
				added = append(added, caption)
			}
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
			indices = append(indices, &types.IndexInfo{Content: buildEntryIndexContent(entry, chunk.Content), SourceID: chunk.ID,
				SourceType: types.ChunkSourceType, ChunkID: chunk.ID, EntryID: entry.ID,
				LibraryID: library.ID, IsEnabled: true})
		}
		if err := s.retrieval.BatchIndex(ctx, embedder, indices); err != nil {
			// Failed new chunks must not be reused as if their vectors were complete.
			ids := make([]string, 0, len(added))
			for _, chunk := range added {
				ids = append(ids, chunk.ID)
			}
			_ = s.retrieval.DeleteByChunkIDList(ctx, ids, 0, entry.Type)
			_ = s.db.WithContext(ctx).Unscoped().Where("space_id=? AND id IN ?", entry.SpaceID, ids).Delete(&types.Chunk{}).Error
			return err
		}
	}
	if len(removed) > 0 {
		if s.retrieval != nil {
			if err := s.retrieval.DeleteByChunkIDList(ctx, removed, 0, entry.Type); err != nil {
				return err
			}
		}
		if err := s.db.WithContext(ctx).Unscoped().Where("space_id=? AND id IN ?", entry.SpaceID, removed).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *VaultDocumentService) deleteDocument(ctx context.Context, entry *types.Entry) error {
	if s.retrieval != nil {
		if err := s.retrieval.DeleteByEntryIDList(ctx, []string{entry.ID}, 0, entry.Type); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("space_id=? AND entry_id=?", entry.SpaceID, entry.ID).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("space_id=? AND id=?", entry.SpaceID, entry.ID).Delete(&types.Entry{}).Error
	})
}

func (s *VaultDocumentService) Status(ctx context.Context) (VaultStatus, error) {
	s.statusMu.RLock()
	result := s.status
	s.statusMu.RUnlock()
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		spaceID = types.SingleSpaceID
	}
	for _, target := range []struct {
		kind  string
		count *int64
	}{{"mine", &result.Mine.IndexedFiles}, {"report", &result.Reports.IndexedFiles}} {
		if err := s.db.WithContext(ctx).Model(&types.Entry{}).
			Where("space_id=? AND source_kind=? AND parse_status=?", spaceID, target.kind, types.ParseStatusCompleted).
			Count(target.count).Error; err != nil {
			return result, err
		}
	}
	return result, nil
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
