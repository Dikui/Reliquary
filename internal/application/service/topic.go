package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// TopicService owns user topic changes and durable historical classification.
type TopicService struct {
	db        *gorm.DB
	models    interfaces.ModelService
	headers   ArticleHeaderRefresher
	logs      *OperationLogService
	batchSize int
	tasks     interfaces.TaskEnqueuer
}

func NewTopicService(db *gorm.DB) *TopicService {
	return &TopicService{db: db, batchSize: 30}
}
func (s *TopicService) SetHeaderRefresher(v ArticleHeaderRefresher) { s.headers = v }
func (s *TopicService) SetOperationLogs(v *OperationLogService)     { s.logs = v }
func (s *TopicService) SetModelService(v interfaces.ModelService)   { s.models = v }
func (s *TopicService) SetBackfillBatchSize(n int) {
	if n > 0 {
		s.batchSize = n
	}
}

func (s *TopicService) requireSpace(ctx context.Context) (uint64, error) {
	id, ok := types.SpaceIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, werrors.NewUnauthorizedError("personal space is required")
	}
	return id, nil
}

func (s *TopicService) ListTopics(ctx context.Context) ([]types.Topic, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	result := []types.Topic{}
	err = s.db.WithContext(ctx).Table("topics t").Select(`t.*, (SELECT count(*) FROM entry_topics kt
		JOIN entries k ON k.id=kt.entry_id AND k.space_id=kt.space_id
		WHERE kt.space_id=t.space_id AND kt.topic_id=t.id AND k.deleted_at IS NULL AND k.source_kind IN ('article','mine')) AS article_count`).
		Where("t.space_id = ?", spaceID).Order("t.name, t.id").Find(&result).Error
	return result, err
}

func validateTopicName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", werrors.NewBadRequestError("topic name is required")
	}
	if len([]rune(name)) > 255 {
		return "", werrors.NewBadRequestError("topic name exceeds 255 characters")
	}
	return name, nil
}

func topicDatabaseError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return werrors.NewConflictError("topic name already exists in this space")
	}
	return err
}
func (s *TopicService) record(ctx context.Context, tx *gorm.DB, action, id string, before, after any) error {
	if s.logs == nil {
		return nil
	}
	return s.logs.RecordTx(ctx, tx, action, id, before, after)
}
func (s *TopicService) refresh(ctx context.Context, ids []string) {
	if s.headers == nil {
		return
	}
	for _, id := range ids {
		s.headers.ScheduleRefresh(ctx, id)
	}
}
func (s *TopicService) SetTaskEnqueuer(queue interfaces.TaskEnqueuer) { s.tasks = queue }

func (s *TopicService) CreateTopic(ctx context.Context, topic *types.Topic) (*types.Topic, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if topic == nil {
		return nil, werrors.NewBadRequestError("topic name is required")
	}
	name, err := validateTopicName(topic.Name)
	if err != nil {
		return nil, err
	}
	copy := *topic
	copy.SpaceID, copy.Name = spaceID, name
	copy.LibraryIDs, err = s.validateLibraries(ctx, spaceID, topic.LibraryIDs)
	if err != nil {
		return nil, err
	}
	if copy.ID == "" {
		copy.ID = uuid.NewString()
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&copy).Error; err != nil {
			return err
		}
		if err := s.enqueueBackfill(tx, &copy); err != nil {
			return err
		}
		return s.record(ctx, tx, "create_topic", copy.ID, nil, copy)
	})
	if err != nil {
		return nil, topicDatabaseError(err)
	}
	return &copy, nil
}

// UpdateTopic reclassifies eligible history after an explanation or scope change.
func (s *TopicService) UpdateTopic(ctx context.Context, id string, name, description *string, scopes ...[]string) (*types.Topic, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if name == nil && description == nil && len(scopes) == 0 {
		return nil, werrors.NewBadRequestError("name or description is required")
	}
	if name != nil {
		value, err := validateTopicName(*name)
		if err != nil {
			return nil, err
		}
		name = &value
	}
	var libraries []string
	if len(scopes) > 0 {
		libraries, err = s.validateLibraries(ctx, spaceID, scopes[0])
		if err != nil {
			return nil, err
		}
	}
	var result types.Topic
	var affected []string
	changed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, id).First(&result).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("topic not found")
			}
			return err
		}
		before := result
		if len(scopes) > 0 {
			result.LibraryIDs = libraries
		}
		if name != nil {
			result.Name = *name
		}
		if description != nil {
			result.Description = *description
		}
		if before.Name == result.Name && before.Description == result.Description && slices.Equal(before.LibraryIDs, result.LibraryIDs) {
			return nil
		}
		changed = true
		if err := tx.Model(&types.EntryTopic{}).Where("space_id=? AND topic_id=?", spaceID, id).Pluck("entry_id", &affected).Error; err != nil {
			return err
		}
		if err := tx.Model(&result).Updates(map[string]any{"name": result.Name, "description": result.Description, "library_ids": gorm.Expr("?::jsonb", topicLibrariesJSON(result.LibraryIDs)), "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		if before.Description != result.Description {
			if err := tx.Where("space_id=? AND topic_id=? AND source=? AND entry_id IN (SELECT id FROM entries WHERE ?::jsonb='[]'::jsonb OR ?::jsonb @> jsonb_build_array(library_id))", spaceID, id, types.TopicSourceModel, topicLibrariesJSON(result.LibraryIDs), topicLibrariesJSON(result.LibraryIDs)).Delete(&types.EntryTopic{}).Error; err != nil {
				return err
			}
		}
		if before.Description != result.Description || !slices.Equal(before.LibraryIDs, result.LibraryIDs) {
			if err := s.enqueueBackfill(tx, &result); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, "update_topic", id, before, result)
	})
	if err != nil {
		return nil, topicDatabaseError(err)
	}
	if changed {
		s.refresh(ctx, affected)
	}
	return &result, nil
}

func (s *TopicService) DeleteTopic(ctx context.Context, id string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	var affected []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topic types.Topic
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, id).First(&topic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("topic not found")
			}
			return err
		}
		if err := tx.Model(&types.EntryTopic{}).Where("space_id=? AND topic_id=?", spaceID, id).Pluck("entry_id", &affected).Error; err != nil {
			return err
		}
		if err := tx.Delete(&topic).Error; err != nil {
			return err
		}
		return s.record(ctx, tx, "delete_topic", id, topic, nil)
	})
	if err == nil {
		s.refresh(ctx, affected)
	}
	return err
}

// ReplaceEntryTopics applies processing output while retaining user selections.
// Unavailable model IDs are ignored because model output cannot create topics.
func (s *TopicService) ReplaceEntryTopics(ctx context.Context, entryID string, ids []string) error {
	return s.replaceEntryTopics(ctx, entryID, ids, types.TopicSourceModel)
}
func (s *TopicService) SetUserEntryTopics(ctx context.Context, entryID string, ids []string) error {
	return s.replaceEntryTopics(ctx, entryID, ids, types.TopicSourceUser)
}
func uniqueTopicIDs(ids []string) []string {
	set := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if !set[id] {
			set[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}
func (s *TopicService) replaceEntryTopics(ctx context.Context, entryID string, ids []string, source string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	ids = uniqueTopicIDs(ids)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var article types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,source_kind,library_id").Where("space_id=? AND id=?", spaceID, entryID).First(&article).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("article not found")
			}
			return err
		}
		if !isIngestArticle(&article) {
			return werrors.NewBadRequestError("only articles can have topics")
		}
		var selected []types.Topic
		if len(ids) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND id IN ? AND (library_ids = '[]'::jsonb OR library_ids @> ?::jsonb)", spaceID, ids, topicLibrariesJSON([]string{article.LibraryID})).Order("id").Find(&selected).Error; err != nil {
				return err
			}
		}
		if source == types.TopicSourceUser && len(selected) != len(ids) {
			return werrors.NewBadRequestError("topic is unavailable in this space")
		}
		var before []types.EntryTopic
		if err := tx.Where("space_id=? AND entry_id=?", spaceID, entryID).Order("topic_id").Find(&before).Error; err != nil {
			return err
		}
		query := tx.Where("space_id=? AND entry_id=?", spaceID, entryID)
		if source == types.TopicSourceModel {
			query = query.Where("source=? AND topic_id IN (SELECT id FROM topics WHERE library_ids = '[]'::jsonb OR library_ids @> ?::jsonb)", types.TopicSourceModel, topicLibrariesJSON([]string{article.LibraryID}))
		}
		if err := query.Delete(&types.EntryTopic{}).Error; err != nil {
			return err
		}
		links := make([]types.EntryTopic, 0, len(selected))
		for _, topic := range selected {
			link := types.EntryTopic{SpaceID: spaceID, EntryID: entryID, TopicID: topic.ID, Source: source}
			for _, previous := range before {
				if previous.TopicID == topic.ID {
					link.CreatedAt, link.IsHistorical = previous.CreatedAt, previous.IsHistorical
					break
				}
			}
			links = append(links, link)
		}
		if len(links) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}, {Name: "topic_id"}}, DoNothing: true}).Create(&links).Error; err != nil {
				return err
			}
		}
		if source == types.TopicSourceUser {
			return s.record(ctx, tx, "correct_topics", entryID, before, links)
		}
		return nil
	})
	if err == nil {
		s.refresh(ctx, []string{entryID})
	}
	return err
}

func topicLibrariesJSON(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}
func (s *TopicService) validateLibraries(ctx context.Context, spaceID uint64, ids []string) ([]string, error) {
	ids = uniqueTopicIDs(ids)
	if len(ids) > 100 {
		return nil, werrors.NewBadRequestError("library_ids accepts at most 100 IDs")
	}
	if len(ids) == 0 {
		return []string{}, nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&types.Library{}).Where("space_id=? AND id IN ?", spaceID, ids).Count(&n).Error; err != nil {
		return nil, err
	}
	if n != int64(len(ids)) {
		return nil, werrors.NewBadRequestError("library_ids contains an unavailable library")
	}
	return ids, nil
}
func (s *TopicService) TopicsForLibrary(ctx context.Context, libraryID string) ([]types.Topic, error) {
	items, err := s.ListTopics(ctx)
	result := []types.Topic{}
	for _, t := range items {
		if len(t.LibraryIDs) == 0 || slices.Contains(t.LibraryIDs, libraryID) {
			result = append(result, t)
		}
	}
	return result, err
}
