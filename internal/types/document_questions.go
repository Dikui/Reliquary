package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// GeneratedQuestion 表示AI生成的单个问题
type GeneratedQuestion struct {
	ID              string `json:"id"`                         // 唯一标识，用于构造 source_id
	Question        string `json:"question"`                   // 问题内容
	ContentRevision *int   `json:"content_revision,omitempty"` // 该问题对应的 Chunk 内容版本
}

const maxGeneratedQuestionSourceIDLength = 64

// GeneratedQuestionSourceID builds the retrieval source identifier for a
// generated question. PostgreSQL stores source_id as varchar(64), while a
// chunk UUID plus a question UUID would be 73 bytes. Preserve the historical
// representation for short IDs and hash only oversized question IDs so
// existing index rows remain addressable by delete/reindex operations.
func GeneratedQuestionSourceID(chunkID, questionID string) string {
	candidate := chunkID + "-" + questionID
	if len(candidate) <= maxGeneratedQuestionSourceIDLength {
		return candidate
	}
	digest := sha256.Sum256([]byte(questionID))
	// UUID chunk IDs use 36 bytes; "-q" plus 24 hex characters keeps the
	// complete identifier at 62 bytes while retaining ample collision space.
	return chunkID + "-q" + hex.EncodeToString(digest[:12])
}

// DocumentChunkMetadata 定义文档 Chunk 的元数据结构
// 用于存储AI生成的问题等增强信息
type DocumentChunkMetadata struct {
	// GeneratedQuestions 存储AI为该Chunk生成的相关问题
	// 这些问题会被独立索引以提高召回率
	GeneratedQuestions []GeneratedQuestion `json:"generated_questions,omitempty"`
	// GeneratedQuestionsRevision ties the questions to Chunk.ContentRevision.
	GeneratedQuestionsRevision int `json:"generated_questions_revision,omitempty"`
}

// IsQuestionCurrent reports whether a generated question was authored for the
// current chunk body. This is advisory metadata for the UI: questions remain
// valid retrieval aliases across chunk edits. Legacy rows fall back to the
// metadata-level revision.
func (m *DocumentChunkMetadata) IsQuestionCurrent(question GeneratedQuestion, chunkRevision int) bool {
	if question.ContentRevision != nil {
		return *question.ContentRevision == chunkRevision
	}
	return m != nil && m.GeneratedQuestionsRevision == chunkRevision
}

// GetQuestionStrings 返回问题内容字符串列表（兼容旧代码）
func (m *DocumentChunkMetadata) GetQuestionStrings() []string {
	if m == nil || len(m.GeneratedQuestions) == 0 {
		return nil
	}
	result := make([]string, len(m.GeneratedQuestions))
	for i, q := range m.GeneratedQuestions {
		result[i] = q.Question
	}
	return result
}

// DocumentMetadata 解析 Chunk 中的文档元数据
func (c *Chunk) DocumentMetadata() (*DocumentChunkMetadata, error) {
	if c == nil || len(c.Metadata) == 0 {
		return nil, nil
	}
	var meta DocumentChunkMetadata
	if err := json.Unmarshal(c.Metadata, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SetDocumentMetadata 设置 Chunk 的文档元数据
func (c *Chunk) SetDocumentMetadata(meta *DocumentChunkMetadata) error {
	if c == nil {
		return nil
	}
	if meta == nil {
		c.Metadata = nil
		return nil
	}
	bytes, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	c.Metadata = JSON(bytes)
	return nil
}
