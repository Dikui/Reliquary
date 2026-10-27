package postgres

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"reliquary/internal/types"
)

// pgVector defines the database model for vector embeddings storage
type pgVector struct {
	ID         uint                `json:"id"                gorm:"primarykey"`
	CreatedAt  time.Time           `json:"created_at"        gorm:"column:created_at"`
	UpdatedAt  time.Time           `json:"updated_at"        gorm:"column:updated_at"`
	SourceID   string              `json:"source_id"         gorm:"column:source_id;not null"`
	SourceType int                 `json:"source_type"       gorm:"column:source_type;not null"`
	ChunkID    string              `json:"chunk_id"          gorm:"column:chunk_id"`
	EntryID    string              `json:"entry_id"      gorm:"column:entry_id"`
	LibraryID  string              `json:"library_id" gorm:"column:library_id"`
	ModelID    string              `json:"model_id" gorm:"column:model_id"`
	Dimension  int                 `json:"dimension"         gorm:"column:dimension;not null"`
	Embedding  pgvector.HalfVector `json:"embedding"         gorm:"column:embedding;not null"`
}

// pgVectorWithScore extends pgVector with similarity score field
type pgVectorWithScore struct {
	ID            uint                `json:"id"                gorm:"primarykey"`
	CreatedAt     time.Time           `json:"created_at"        gorm:"column:created_at"`
	UpdatedAt     time.Time           `json:"updated_at"        gorm:"column:updated_at"`
	SourceID      string              `json:"source_id"         gorm:"column:source_id;not null"`
	SourceType    int                 `json:"source_type"       gorm:"column:source_type;not null"`
	ChunkID       string              `json:"chunk_id"          gorm:"column:chunk_id"`
	EntryID       string              `json:"entry_id"      gorm:"column:entry_id"`
	LibraryID     string              `json:"library_id" gorm:"column:library_id"`
	Content       string              `json:"content"           gorm:"column:content;->"`
	ContextHeader string              `json:"-"                 gorm:"column:context_header;->"`
	Title         string              `json:"-"                 gorm:"column:title;->"`
	Dimension     int                 `json:"dimension"         gorm:"column:dimension;not null"`
	Embedding     pgvector.HalfVector `json:"embedding"         gorm:"column:embedding;not null"`
	Score         float64             `json:"score"             gorm:"column:score"`
}

// TableName specifies the database table name for pgVectorWithScore
func (pgVectorWithScore) TableName() string {
	return "embeddings"
}

// TableName specifies the database table name for pgVector
func (pgVector) TableName() string {
	return "embeddings"
}

// toDBVectorEmbedding converts IndexInfo to pgVector database model
func toDBVectorEmbedding(indexInfo *types.IndexInfo, additionalParams map[string]any) *pgVector {
	pgVector := &pgVector{
		SourceID:   indexInfo.SourceID,
		SourceType: int(indexInfo.SourceType),
		ChunkID:    indexInfo.ChunkID,
		EntryID:    indexInfo.EntryID,
		LibraryID:  indexInfo.LibraryID,
	}
	// Add embedding data if available in additionalParams
	if additionalParams != nil && slices.Contains(slices.Collect(maps.Keys(additionalParams)), "embedding") {
		if embeddingMap, ok := additionalParams["embedding"].(map[string][]float32); ok {
			pgVector.Embedding = pgvector.NewHalfVector(embeddingMap[indexInfo.SourceID])
			pgVector.Dimension = len(pgVector.Embedding.Slice())
		}
	}
	// Get model_id from additionalParams if available
	if additionalParams != nil {
		if modelID, ok := additionalParams["model_id"].(string); ok {
			pgVector.ModelID = modelID
		}
	}
	return pgVector
}

// fromDBVectorEmbeddingWithScore converts pgVectorWithScore to IndexWithScore domain model
func fromDBVectorEmbeddingWithScore(embedding *pgVectorWithScore, matchType types.MatchType) *types.IndexWithScore {
	content := strings.TrimSpace(embedding.Content)
	if embedding.SourceID != embedding.ChunkID {
		return nil
	}
	if embedding.ContextHeader != "" {
		content = embedding.ContextHeader + "\n\n" + content
	}
	if title := strings.TrimSpace(embedding.Title); title != "" {
		content = title + "\n" + content
	}
	return &types.IndexWithScore{
		ID:         strconv.FormatInt(int64(embedding.ID), 10),
		SourceID:   embedding.SourceID,
		SourceType: types.SourceType(embedding.SourceType),
		ChunkID:    embedding.ChunkID,
		EntryID:    embedding.EntryID,
		LibraryID:  embedding.LibraryID,
		Content:    content,
		Score:      embedding.Score,
		MatchType:  matchType,
	}
}
