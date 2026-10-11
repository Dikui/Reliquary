package service

import (
	"context"
	"errors"
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

func (s *TopicService) requireTenant(ctx context.Context) (uint64, error) {
	id, ok := types.TenantIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, werrors.NewUnauthorizedError("personal space is required")
	}
	return id, nil
}

func (s *TopicService) ListTopics(ctx context.Context) ([]types.Topic, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	result := []types.Topic{}
	err = s.db.WithContext(ctx).Table("topics t").Select(`t.*, (SELECT count(*) FROM knowledge_topics kt
		JOIN knowledges k ON k.id=kt.knowledge_id AND k.tenant_id=kt.tenant_id
		WHERE kt.tenant_id=t.tenant_id AND kt.topic_id=t.id AND k.deleted_at IS NULL AND k.source_kind='article') AS article_count`).
		Where("t.tenant_id = ?", tenantID).Order("t.name, t.id").Find(&result).Error
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
	tenantID, err := s.requireTenant(ctx)
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
	copy.TenantID, copy.Name = tenantID, name
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

// UpdateTopic reruns classification only when the explanation changes.
func (s *TopicService) UpdateTopic(ctx context.Context, id string, name, description *string) (*types.Topic, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	if name == nil && description == nil {
		return nil, werrors.NewBadRequestError("name or description is required")
	}
	if name != nil {
		value, err := validateTopicName(*name)
		if err != nil {
			return nil, err
		}
		name = &value
	}
	var result types.Topic
	var affected []string
	changed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenantID, id).First(&result).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("topic not found")
			}
			return err
		}
		before := result
		if name != nil {
			result.Name = *name
		}
		if description != nil {
			result.Description = *description
		}
		if before.Name == result.Name && before.Description == result.Description {
			return nil
		}
		changed = true
		if err := tx.Model(&types.KnowledgeTopic{}).Where("tenant_id=? AND topic_id=?", tenantID, id).Pluck("knowledge_id", &affected).Error; err != nil {
			return err
		}
		if err := tx.Model(&result).Updates(map[string]any{"name": result.Name, "description": result.Description, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		if before.Description != result.Description {
			if err := tx.Where("tenant_id=? AND topic_id=? AND source=?", tenantID, id, types.TopicSourceModel).Delete(&types.KnowledgeTopic{}).Error; err != nil {
				return err
			}
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
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	var affected []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topic types.Topic
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenantID, id).First(&topic).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("topic not found")
			}
			return err
		}
		if err := tx.Model(&types.KnowledgeTopic{}).Where("tenant_id=? AND topic_id=?", tenantID, id).Pluck("knowledge_id", &affected).Error; err != nil {
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

// ReplaceKnowledgeTopics applies processing output while retaining user selections.
// Unavailable model IDs are ignored because model output cannot create topics.
func (s *TopicService) ReplaceKnowledgeTopics(ctx context.Context, knowledgeID string, ids []string) error {
	return s.replaceKnowledgeTopics(ctx, knowledgeID, ids, types.TopicSourceModel)
}
func (s *TopicService) SetUserKnowledgeTopics(ctx context.Context, knowledgeID string, ids []string) error {
	return s.replaceKnowledgeTopics(ctx, knowledgeID, ids, types.TopicSourceUser)
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
func (s *TopicService) replaceKnowledgeTopics(ctx context.Context, knowledgeID string, ids []string, source string) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	ids = uniqueTopicIDs(ids)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var article types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,source_kind").Where("tenant_id=? AND id=?", tenantID, knowledgeID).First(&article).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("article not found")
			}
			return err
		}
		if article.SourceKind != "article" {
			return werrors.NewBadRequestError("only articles can have topics")
		}
		var selected []types.Topic
		if len(ids) > 0 {
			if err := tx.Where("tenant_id=? AND id IN ?", tenantID, ids).Order("id").Find(&selected).Error; err != nil {
				return err
			}
		}
		if source == types.TopicSourceUser && len(selected) != len(ids) {
			return werrors.NewBadRequestError("topic is unavailable in this space")
		}
		var before []types.KnowledgeTopic
		if err := tx.Where("tenant_id=? AND knowledge_id=?", tenantID, knowledgeID).Order("topic_id").Find(&before).Error; err != nil {
			return err
		}
		query := tx.Where("tenant_id=? AND knowledge_id=?", tenantID, knowledgeID)
		if source == types.TopicSourceModel {
			query = query.Where("source=?", types.TopicSourceModel)
		}
		if err := query.Delete(&types.KnowledgeTopic{}).Error; err != nil {
			return err
		}
		links := make([]types.KnowledgeTopic, 0, len(selected))
		for _, topic := range selected {
			link := types.KnowledgeTopic{TenantID: tenantID, KnowledgeID: knowledgeID, TopicID: topic.ID, Source: source}
			for _, previous := range before {
				if previous.TopicID == topic.ID {
					link.CreatedAt, link.IsHistorical = previous.CreatedAt, previous.IsHistorical
					break
				}
			}
			links = append(links, link)
		}
		if len(links) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "knowledge_id"}, {Name: "topic_id"}}, DoNothing: true}).Create(&links).Error; err != nil {
				return err
			}
		}
		if source == types.TopicSourceUser {
			return s.record(ctx, tx, "correct_topics", knowledgeID, before, links)
		}
		return nil
	})
	if err == nil {
		s.refresh(ctx, []string{knowledgeID})
	}
	return err
}
