// Package types defines data structures and types used throughout the system
// These types are shared across different service modules to ensure data consistency
package types

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// ChunkType 定义了不同类型的 Chunk
type ChunkType = string

const (
	// ChunkTypeText 表示普通的文本 Chunk
	ChunkTypeText ChunkType = "text"
	// ChunkTypeParentText 表示父子分块策略中的父文本 Chunk（仅用于上下文，不参与向量索引）
	ChunkTypeParentText ChunkType = "parent_text"
	// ChunkTypeSummary 表示摘要类型的 Chunk
	ChunkTypeSummary = "summary"
	// ChunkTypeEntity 表示实体类型的 Chunk
	ChunkTypeEntity ChunkType = "entity"
	// ChunkTypeRelationship 表示关系类型的 Chunk
	ChunkTypeRelationship ChunkType = "relationship"
	// ChunkTypeTableSummary 表示数据表摘要的 Chunk
	ChunkTypeTableSummary ChunkType = "table_summary"
	// ChunkTypeTableColumn 表示数据表列描述的 Chunk
	ChunkTypeTableColumn ChunkType = "table_column"
)

// ChunkStatus 定义了不同状态的 Chunk
type ChunkStatus int

const (
	ChunkStatusDefault ChunkStatus = 0
	// ChunkStatusStored 表示已存储的 Chunk
	ChunkStatusStored ChunkStatus = 1
	// ChunkStatusIndexed 表示已索引的 Chunk
	ChunkStatusIndexed ChunkStatus = 2
)

// ChunkFlags 保留已有分块状态字段的存储类型。
type ChunkFlags int

// ImageInfo 表示与 Chunk 关联的图片信息
type ImageInfo struct {
	// 图片URL
	URL string `json:"url"          gorm:"type:text"`
	// 原始图片URL
	OriginalURL string `json:"original_url" gorm:"type:text"`
	// 图片在文本中的开始位置
	StartPos int `json:"start_pos"`
	// 图片在文本中的结束位置
	EndPos int `json:"end_pos"`
	// 图片描述
	Caption string `json:"caption"`
	// 图片OCR文本
	OCRText string `json:"ocr_text"`
}

// VideoInfo 表示与 Chunk 关联的视频信息
type VideoInfo struct {
	// 视频URL
	URL string `json:"url"          gorm:"type:text"`
}

// Chunk represents a document chunk
// Chunks are meaningful text segments extracted from original documents
// and are the basic units of library retrieval
// Each chunk contains a portion of the original content
// and maintains its positional relationship with the original text
// Chunks can be independently embedded as vectors and retrieved, supporting precise content localization
type Chunk struct {
	SourceLocations []SourceLocation `json:"source_locations" gorm:"serializer:json;type:jsonb"`
	// Unique identifier of the chunk, using UUID format
	ID    string `json:"id"                       gorm:"type:varchar(36);primaryKey"`
	SeqID int64  `json:"seq_id"                   gorm:"type:bigint;uniqueIndex;autoIncrement"`
	// Space ID, used for multi-space isolation
	SpaceID uint64 `json:"space_id"`
	// ID of the parent entry, associated with the Entry model
	EntryID string `json:"entry_id"`
	// ID of the library, for quick location
	LibraryID string `json:"library_id"`
	// Actual text content of the chunk
	Content string `json:"content"`
	// Index position of the chunk in the original document
	ChunkIndex int `json:"chunk_index"`
	// Whether the chunk is enabled, can be used to temporarily disable certain chunks
	IsEnabled bool `json:"is_enabled"               gorm:"default:true"`
	// Flags 保留存储列、API 字段及原默认值；当前检索不读取此字段。
	Flags ChunkFlags `json:"flags"                    gorm:"default:1"`
	// Status of the chunk
	Status int `json:"status"                   gorm:"default:0"`
	// Starting character position in the original text
	StartAt int `json:"start_at"`
	// Ending character position in the original text
	EndAt int `json:"end_at"`
	// Previous chunk ID
	PreChunkID string `json:"pre_chunk_id"`
	// Next chunk ID
	NextChunkID string `json:"next_chunk_id"`
	// Chunk 类型，用于区分不同类型的 Chunk
	ChunkType ChunkType `json:"chunk_type"               gorm:"type:varchar(20);default:'text'"`
	// 父 Chunk ID，用于关联图片 Chunk 和原始文本 Chunk
	ParentChunkID string `json:"parent_chunk_id"          gorm:"type:varchar(36);index"`
	// 关系 Chunk ID，用于关联关系 Chunk 和原始文本 Chunk
	RelationChunks JSON `json:"relation_chunks"          gorm:"type:json"`
	// 间接关系 Chunk ID，用于关联间接关系 Chunk 和原始文本 Chunk
	IndirectRelationChunks JSON   `json:"indirect_relation_chunks" gorm:"type:json"`
	Metadata               JSON   `json:"metadata"                 gorm:"type:json"`
	ContentHash            string `json:"content_hash"             gorm:"type:varchar(64);index"`
	// 图片信息，存储为 JSON
	ImageInfo string `json:"image_info"               gorm:"type:text"`
	// Chunk creation time
	CreatedAt time.Time `json:"created_at"`
	// Chunk last update time
	UpdatedAt time.Time `json:"updated_at"`
	// Soft delete marker, supports data recovery
	DeletedAt gorm.DeletedAt `json:"deleted_at"               gorm:"index"`
	// ContextHeader is a Markdown heading breadcrumb prepended when indexing.
	ContextHeader string `json:"-" gorm:"type:text"`
}

// EmbeddingContent returns the chunk content with ContextHeader prepended
// when set. Use this where the embedding model needs section context that
// isn't part of the literal Content. Surrounding whitespace on Content is
// trimmed so leading/trailing newlines from boundary slicing don't dilute
// the embedded vector.
func (c *Chunk) EmbeddingContent() string {
	if c == nil {
		return ""
	}
	body := strings.TrimSpace(c.Content)
	if c.ContextHeader == "" {
		return body
	}
	return c.ContextHeader + "\n\n" + body
}

// BeforeSave keeps absent generated-text locations as an empty JSON array.
func (c *Chunk) BeforeSave(tx *gorm.DB) error {
	if c.EntryID != "" && c.Content != "" && c.SourceLocations == nil {
		var entry Entry
		if err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Select("vault_body").Where("id=? AND space_id=?", c.EntryID, c.SpaceID).Find(&entry).Error; err != nil {
			return err
		}
		if entry.VaultBody != "" {
			AssignStoredBodyLocations(entry.VaultBody, c)
		}
	}
	if c.SourceLocations == nil {
		c.SourceLocations = []SourceLocation{}
	}
	return nil
}
