package service

import (
	"context"
	"errors"
	"os"
	"sync"

	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	"reliquary/internal/maintenanceops"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// transactionTaskEnqueuer binds every task created by a mutation to its
// database transaction. A failed enqueue aborts the transaction even if a
// legacy caller turns that failure into a response containing an article.
type transactionTaskEnqueuer struct {
	queue         interfaces.TransactionalTaskEnqueuer
	ctx           context.Context
	tx            *gorm.DB
	enqueueErr    error
	commitFailure bool
}

type entryBatchChildKey struct{}

func (q *transactionTaskEnqueuer) Enqueue(task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	info, err := q.queue.EnqueueTx(q.ctx, q.tx, task, opts...)
	if err == nil && task.Type() == types.TypeManualProcess {
		if capture, ok := q.ctx.Value(entryBatchChildKey{}).(func(*gorm.DB, *taskqueue.TaskInfo) error); ok {
			err = capture(q.tx, info)
		}
	}
	if err != nil {
		q.enqueueErr = errors.Join(q.enqueueErr, err)
	}
	return info, err
}
func (s *entryService) needsTaskTransaction() bool {
	if s.db == nil {
		return false
	}
	_, bound := s.task.(*transactionTaskEnqueuer)
	return !bound
}
func entryTaskTransaction[T any](ctx context.Context, s *entryService, mutation func(*entryService) (T, error)) (result T, err error) {
	queue, ok := s.task.(interfaces.TransactionalTaskEnqueuer)
	if !ok {
		return result, errors.New("task queue must support PostgreSQL transactions")
	}
	var responseErr error
	var rollbackArticle *types.Entry
	var storedPaths []string
	var storedPathsMu sync.Mutex
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := maintenanceops.CheckCollectionTx(ctx, tx); err != nil {
			return err
		}
		copy := *s
		copy.db = tx
		copy.repo = repository.NewEntryRepository(tx)
		copy.chunkRepo = repository.NewChunkRepository(tx)
		copy.spaceRepo = repository.NewSpaceRepository(tx)
		copy.retrieveEngine = s.contentTypeTransactionService(tx).retrieveEngine
		copy.chunkService = NewChunkService(copy.chunkRepo, copy.repo, repository.NewLibraryRepository(tx), copy.modelService, copy.retrieveEngine)
		if _, ok := s.resourceCatalog.(*resourceCatalog); ok {
			copy.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(tx))
			copy.fileSvc = file.WithResourceCatalog(s.fileSvc, copy.resourceCatalog, func(path string) {
				storedPathsMu.Lock()
				defer storedPathsMu.Unlock()
				storedPaths = append(storedPaths, path)
			})
		}
		if _, ok := s.spanTracker.(*spanTracker); ok {
			copy.spanTracker = NewSpanTracker(repository.NewEntrySpanRepository(tx), tx)
		}
		bound := &transactionTaskEnqueuer{queue: queue, ctx: ctx, tx: tx}
		copy.task = bound
		result, responseErr = mutation(&copy)
		if bound.enqueueErr != nil {
			article, _ := any(result).(*types.Entry)
			if ingest, ok := any(result).(*types.IngestResult); ok && ingest != nil && ingest.EntryID != "" {
				var created types.Entry
				if queryErr := tx.Where("id=?", ingest.EntryID).First(&created).Error; queryErr == nil {
					article = &created
				}
			}
			if article != nil {
				var existed int64
				if queryErr := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=?", article.ID).Count(&existed).Error; queryErr == nil && existed == 0 {
					rollbackArticle = article
				}
			}
			return bound.enqueueErr
		}
		// A duplicate collection still records its additional source atomically.
		var duplicate *types.DuplicateEntryError
		if errors.As(responseErr, &duplicate) || bound.commitFailure {
			return nil
		}
		return responseErr
	})
	if err != nil {
		storedPathsMu.Lock()
		rollbackStoredPaths := append([]string(nil), storedPaths...)
		storedPathsMu.Unlock()
		if len(rollbackStoredPaths) > 0 {
			seen := map[string]bool{}
			for _, path := range rollbackStoredPaths {
				if seen[path] {
					continue
				}
				seen[path] = true
				// Resource rows have already rolled back, so use the captured
				// physical path instead of resolving a resource reference.
				if cleanupErr := s.fileSvc.DeleteFile(ctx, path); !errors.Is(cleanupErr, os.ErrNotExist) {
					err = errors.Join(err, cleanupErr)
				}
			}
		} else if rollbackArticle != nil {
			err = errors.Join(err, s.removeRolledBackCollectionSource(ctx, rollbackArticle))
		}
		var zero T
		return zero, err
	}
	return result, responseErr
}
func (s *entryService) taskEnqueueFailedInTransaction() bool {
	bound, ok := s.task.(*transactionTaskEnqueuer)
	return ok && bound.enqueueErr != nil
}

func (s *entryService) recordTerminalMutationFailure() {
	if bound, ok := s.task.(*transactionTaskEnqueuer); ok {
		bound.commitFailure = true
	}
}

func (s *entryService) removeRolledBackCollectionSource(ctx context.Context, k *types.Entry) error {
	intake, err := k.IntakeMetadata()
	if err != nil {
		return err
	}
	refs := []string{k.FilePath}
	if intake != nil {
		refs = append(append(refs, intake.OriginalPath), intake.Images...)
	}
	seen := map[string]bool{}
	releaser := entryResourceOwners(s.resourceCatalog, k.ID)
	for _, ref := range refs {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		if releaser.deletable(ctx, ref) {
			err = errors.Join(err, s.resolveFileServiceForPath(ctx, nil, ref).DeleteFile(ctx, ref))
		}
	}
	return err
}

// Coordinate user mutations with the operation executor without changing the
// business handler or its checkpoints.
func (s *entryService) withTaskObject(ctx context.Context, id string, mutation func(context.Context) error) error {
	if q, ok := s.task.(interfaces.TaskObjectCoordinator); ok {
		return q.WithObject(ctx, id, mutation)
	}
	if bound, ok := s.task.(*transactionTaskEnqueuer); ok {
		if q, ok := bound.queue.(interfaces.TaskObjectCoordinator); ok {
			return q.WithObject(ctx, id, mutation)
		}
	}
	return mutation(ctx)
}
