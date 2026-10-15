package types

import (
	"context"
	"encoding/json"
	"time"
)

type SearchRecord struct {
	ID               string          `json:"id" gorm:"primaryKey"`
	SpaceID          uint64          `json:"space_id"`
	CreatedAt        time.Time       `json:"created_at"`
	Source           string          `json:"source"`
	Method           string          `json:"method"`
	Query            string          `json:"query"`
	Libraries        []string        `json:"libraries" gorm:"serializer:json"`
	Request          json.RawMessage `json:"request" gorm:"type:jsonb"`
	Results          []RankedEntry   `json:"results" gorm:"serializer:json"`
	DurationMS       int64           `json:"duration_ms"`
	FallbackFulltext bool            `json:"fallback_fulltext"`
	Reranked         bool            `json:"reranked"`
	EvaluationRunID  string          `json:"evaluation_run_id"`
	Error            string          `json:"error"`
}
type RankedEntry struct {
	EntryID string  `json:"entry_id"`
	Rank    int     `json:"rank"`
	Score   float64 `json:"score"`
}
type EvaluationSet struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	SpaceID     uint64    `json:"space_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type EvaluationAnswer struct {
	QuestionID string `json:"-" gorm:"primaryKey"`
	EntryID    string `json:"entry_id" gorm:"primaryKey"`
	Grade      int    `json:"grade"`
}

func (a *EvaluationAnswer) UnmarshalJSON(data []byte) error {
	type plain EvaluationAnswer
	value := plain{Grade: 1}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*a = EvaluationAnswer(value)
	return nil
}

type EvaluationQuestion struct {
	ID               string             `json:"id" gorm:"primaryKey"`
	SpaceID          uint64             `json:"space_id"`
	SetID            string             `json:"set_id"`
	Method           string             `json:"method"`
	Request          EntrySearchRequest `json:"request" gorm:"serializer:json"`
	Source           string             `json:"source"`
	Status           string             `json:"status"`
	SourceRecordID   string             `json:"source_record_id"`
	SuggestedResults []RankedEntry      `json:"suggested_results" gorm:"serializer:json"`
	AnswerDeleted    bool               `json:"answer_deleted"`
	Revision         int                `json:"revision"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	Answers          []EvaluationAnswer `json:"answers" gorm:"foreignKey:QuestionID"`
}
type EvaluationRunOptions struct {
	ExpectedEmbeddingModelID     string `json:"-"`
	ExpectedEmbeddingProfileHash string `json:"-"`
	K                            int    `json:"k"`
	// active, candidate:<rebuild ID>, or archive:<switched rebuild ID>.
	VectorSet       string           `json:"vector_set"`
	CollapseSimilar *bool            `json:"collapse_similar,omitempty"`
	RetrievalConfig *RetrievalConfig `json:"retrieval_config,omitempty"`
	RerankModelID   *string          `json:"rerank_model_id,omitempty"`
}
type EvaluationMetrics struct {
	Precision float64 `json:"precision_at_k"`
	Recall    float64 `json:"recall_at_k"`
	MRR       float64 `json:"mrr"`
	NDCG      float64 `json:"ndcg_at_k"`
	MAP       float64 `json:"map"`
}
type EvaluationRun struct {
	Snapshot    json.RawMessage       `json:"snapshot" gorm:"type:jsonb"`
	ID          string                `json:"id" gorm:"primaryKey"`
	SpaceID     uint64                `json:"space_id"`
	SetID       string                `json:"set_id"`
	SetSnapshot EvaluationSet         `json:"set_snapshot" gorm:"serializer:json"`
	Options     EvaluationRunOptions  `json:"options" gorm:"serializer:json"`
	Status      string                `json:"status"`
	Summary     EvaluationMetrics     `json:"summary" gorm:"serializer:json"`
	Error       string                `json:"error"`
	CreatedAt   time.Time             `json:"created_at"`
	FinishedAt  *time.Time            `json:"finished_at"`
	Results     []EvaluationRunResult `json:"questions,omitempty" gorm:"foreignKey:RunID"`
}
type EvaluationRunResult struct {
	RunID      string               `json:"run_id" gorm:"primaryKey"`
	QuestionID string               `json:"question_id" gorm:"primaryKey"`
	Question   EvaluationQuestion   `json:"question" gorm:"serializer:json"`
	Snapshot   json.RawMessage      `json:"snapshot" gorm:"type:jsonb"`
	Results    *EntrySearchResponse `json:"results" gorm:"serializer:json"`
	Metrics    EvaluationMetrics    `json:"metrics" gorm:"serializer:json"`
	Hits       []string             `json:"hits" gorm:"serializer:json"`
	Error      string               `json:"error"`
}

type searchOriginKey struct{}
type evaluationRunKey struct{}
type searchExecutionKey struct{}

func WithSearchSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, searchOriginKey{}, source)
}
func SearchSource(ctx context.Context) string {
	if id := EvaluationRunID(ctx); id != "" {
		return "evaluation"
	}
	if s, ok := ctx.Value(searchOriginKey{}).(string); ok {
		return s
	}
	if p, ok := PrincipalFromContext(ctx); ok && p.Type == PrincipalWebUser {
		return "web"
	}
	return "http"
}
func WithEvaluationRun(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, evaluationRunKey{}, id)
}
func EvaluationRunID(ctx context.Context) string {
	s, _ := ctx.Value(evaluationRunKey{}).(string)
	return s
}
func WithSearchExecution(ctx context.Context, options EvaluationRunOptions) context.Context {
	return context.WithValue(ctx, searchExecutionKey{}, options)
}
func SearchExecution(ctx context.Context) (EvaluationRunOptions, bool) {
	v, ok := ctx.Value(searchExecutionKey{}).(EvaluationRunOptions)
	return v, ok
}
