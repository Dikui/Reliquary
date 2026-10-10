package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// TopicService provides the topic catalog and article associations used by processing.
type TopicService struct{ db *gorm.DB }

func NewTopicService(db *gorm.DB) *TopicService { return &TopicService{db: db} }

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
	err = s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("name, id").Find(&result).Error
	return result, err
}

// CreateTopic supports setup and tests; no topic management route is exposed yet.
func (s *TopicService) CreateTopic(ctx context.Context, topic *types.Topic) (*types.Topic, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	if topic == nil || strings.TrimSpace(topic.Name) == "" {
		return nil, werrors.NewBadRequestError("topic name is required")
	}
	if len([]rune(strings.TrimSpace(topic.Name))) > 255 {
		return nil, werrors.NewBadRequestError("topic name exceeds 255 characters")
	}
	copy := *topic
	copy.TenantID = tenantID
	copy.Name = strings.TrimSpace(copy.Name)
	if copy.ID == "" {
		copy.ID = uuid.NewString()
	}
	if err := s.db.WithContext(ctx).Create(&copy).Error; err != nil {
		return nil, err
	}
	return &copy, nil
}

// ReplaceKnowledgeTopics drops unavailable IDs and atomically replaces the article selection.
func (s *TopicService) ReplaceKnowledgeTopics(ctx context.Context, knowledgeID string, ids []string) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var article types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id=? AND id=?", tenantID, knowledgeID).First(&article).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("article not found")
			}
			return err
		}
		if err := tx.Where("tenant_id=? AND knowledge_id=?", tenantID, knowledgeID).Delete(&types.KnowledgeTopic{}).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		var selected []types.Topic
		if err := tx.Where("tenant_id=? AND id IN ?", tenantID, ids).Order("id").Find(&selected).Error; err != nil {
			return err
		}
		if len(selected) == 0 {
			return nil
		}
		links := make([]types.KnowledgeTopic, 0, len(selected))
		for _, topic := range selected {
			links = append(links, types.KnowledgeTopic{TenantID: tenantID, KnowledgeID: knowledgeID, TopicID: topic.ID})
		}
		return tx.Create(&links).Error
	})
}
