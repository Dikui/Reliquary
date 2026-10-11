package types

import "time"

// KnowledgeRepostSource preserves the provenance of a duplicate message without
// indexing or extracting another copy of its body.
type KnowledgeRepostSource struct {
	ID                 string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID            uint64    `json:"space_id" gorm:"not null;uniqueIndex:idx_repost_origin"`
	KnowledgeBaseID    string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null"`
	KnowledgeID        string    `json:"knowledge_id" gorm:"type:varchar(36);not null;index;uniqueIndex:idx_repost_origin"`
	OriginalURL        string    `json:"original_url" gorm:"type:text;not null;default:''"`
	NormalizedURL      string    `json:"-" gorm:"type:text;not null;default:''"`
	SourceFingerprint  string    `json:"-" gorm:"type:char(64);not null;uniqueIndex:idx_repost_origin"`
	Author             string    `json:"author,omitempty" gorm:"type:text;not null;default:''"`
	Publisher          string    `json:"publisher,omitempty" gorm:"type:text;not null;default:''"`
	PublishedAt        string    `json:"published_at,omitempty" gorm:"type:text;not null;default:''"`
	PublishedAtMissing bool      `json:"published_at_missing" gorm:"not null;default:false"`
	MatchMethod        string    `json:"match_method" gorm:"type:varchar(16);not null"`
	SimHashDistance    int       `json:"simhash_distance" gorm:"not null;default:0"`
	CreatedAt          time.Time `json:"created_at"`
}

func (KnowledgeRepostSource) TableName() string { return "knowledge_repost_sources" }
