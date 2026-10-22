package types

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

// ModelCallPurpose is the stable accounting vocabulary shared by all callers.
type ModelCallPurpose string

const (
	CallPurposeIntakeReview      ModelCallPurpose = "intake_review"
	CallPurposeArticleProcessing ModelCallPurpose = "article_processing"
	CallPurposeSegmentMerge      ModelCallPurpose = "segment_merge"
	CallPurposeImageDescription  ModelCallPurpose = "image_description"
	CallPurposeEntityResolution  ModelCallPurpose = "entity_resolution"
	CallPurposeEntityEmbedding   ModelCallPurpose = "entity_name_embedding"
	CallPurposeBodyEmbedding     ModelCallPurpose = "body_embedding"
	CallPurposeRerank            ModelCallPurpose = "rerank"
	CallPurposeEvaluation        ModelCallPurpose = "evaluation_generation"
	CallPurposeContentCheck      ModelCallPurpose = "content_check"
	CallPurposeOther             ModelCallPurpose = "other"
)

var ModelCallPurposes = []ModelCallPurpose{CallPurposeIntakeReview, CallPurposeArticleProcessing, CallPurposeSegmentMerge, CallPurposeImageDescription, CallPurposeEntityResolution, CallPurposeEntityEmbedding, CallPurposeBodyEmbedding, CallPurposeRerank, CallPurposeEvaluation, CallPurposeContentCheck, CallPurposeOther}

func NormalizeModelCallPurpose(stage string, tier ModelTier) ModelCallPurpose {
	switch stage {
	case "content_check":
		return CallPurposeContentCheck
	case "intake_review":
		return CallPurposeIntakeReview
	case "article_processing", "article_graph_verification":
		return CallPurposeArticleProcessing
	case "article_summary_merge", "segment_merge":
		return CallPurposeSegmentMerge
	case "image_description", "image_caption":
		return CallPurposeImageDescription
	case "entity_judgment", "entity_resolution":
		return CallPurposeEntityResolution
	case "entity_embedding", "entity_embedding_rebuild", "entity_name_embedding":
		return CallPurposeEntityEmbedding
	case "document_embedding", "article_embedding", "embedding_rebuild", "body_embedding":
		return CallPurposeBodyEmbedding
	case "evaluation_generation", "evaluation_ai_generation":
		return CallPurposeEvaluation
	case "rerank":
		return CallPurposeRerank
	}
	if tier == ModelTierRerank {
		return CallPurposeRerank
	}
	return CallPurposeOther
}

// Prices are currency units per million tokens. Nil means unconfigured.
type ModelPricing struct {
	InputPerMillion      *float64 `json:"input_per_million" yaml:"input_per_million"`
	ImageInputPerMillion *float64 `json:"image_input_per_million" yaml:"image_input_per_million"`
	OutputPerMillion     *float64 `json:"output_per_million" yaml:"output_per_million"`
	Currency             string   `json:"currency" yaml:"currency"`
}

func (p ModelPricing) Validate() error {
	for _, v := range []*float64{p.InputPerMillion, p.ImageInputPerMillion, p.OutputPerMillion} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 1e9) {
			return fmt.Errorf("token prices must be finite and between 0 and 1000000000")
		}
	}
	if p.Currency != "" && (len(p.Currency) != 3 || strings.Trim(p.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
		return fmt.Errorf("currency must be a three-letter uppercase code")
	}
	return nil
}

type ModelCall struct {
	ProcessingAttempt int               `json:"processing_attempt"`
	SpanID            string            `json:"span_id"`
	Content           *ModelCallContent `json:"-" gorm:"-"`
	ID                string            `json:"id" gorm:"primaryKey"`
	SpaceID           uint64            `json:"space_id"`
	RequestedAt       time.Time         `json:"requested_at"`
	ModelID           string            `json:"model_id"`
	ModelName         string            `json:"model_name"`
	Provider          string            `json:"provider"`
	Purpose           ModelCallPurpose  `json:"purpose"`
	EntryID           *string           `json:"entry_id"`
	InputTokens       *int64            `json:"input_tokens"`
	TextInputTokens   *int64            `json:"text_input_tokens"`
	ImageInputTokens  *int64            `json:"image_input_tokens"`
	OutputTokens      *int64            `json:"output_tokens"`
	InputPrice        *float64          `json:"input_price_per_million"`
	ImageInputPrice   *float64          `json:"image_input_price_per_million"`
	OutputPrice       *float64          `json:"output_price_per_million"`
	Currency          string            `json:"currency"`
	Cost              *float64          `json:"cost"`
	CostStatus        string            `json:"cost_status"`
	DurationMS        int64             `json:"duration_ms"`
	Success           bool              `json:"success"`
	ErrorCategory     string            `json:"error_category"`
	Attempt           int               `json:"attempt"`
	RetryNumber       int               `json:"retry_number"`
}

type ModelCallAccounting struct {
	Model *Model
	Write func(context.Context, *ModelCall) error
}
type callAccountingKey struct{}
type callEntryKey struct{}
type callRetryKey struct{}
type ModelCallSession struct {
	Accounting *ModelCallAccounting
	Attempts   atomic.Int32
}

func WithModelCallAccounting(ctx context.Context, a *ModelCallAccounting) context.Context {
	if a == nil {
		return ctx
	}
	return context.WithValue(ctx, callAccountingKey{}, &ModelCallSession{Accounting: a})
}
func ModelCallSessionFromContext(ctx context.Context) *ModelCallSession {
	s, _ := ctx.Value(callAccountingKey{}).(*ModelCallSession)
	return s
}
func WithModelCallEntry(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callEntryKey{}, id)
}
func ModelCallEntryFromContext(ctx context.Context) *string {
	id, _ := ctx.Value(callEntryKey{}).(string)
	if id == "" {
		return nil
	}
	return &id
}

// RetryNumber is the enclosing business retry (zero for the first execution).
func WithModelCallRetry(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, callRetryKey{}, n)
}
func ModelCallRetryFromContext(ctx context.Context) int {
	n, _ := ctx.Value(callRetryKey{}).(int)
	return n
}

// ModelCallContent is loaded separately from the accounting list.
type ModelCallContent struct {
	ID            string `json:"id"`
	Request       string `json:"request"`
	Response      string `json:"response"`
	RequestBytes  int64  `json:"request_bytes"`
	ResponseBytes int64  `json:"response_bytes"`
	ResponseType  string `json:"response_type"`
	HTTPStatus    int    `json:"http_status"`
	Complete      bool   `json:"complete"`
}
type modelCallTraceKey struct{}
type ModelCallTrace struct {
	Attempt int
	SpanID  string
}

func WithModelCallTrace(ctx context.Context, attempt int, spanID string) context.Context {
	return context.WithValue(ctx, modelCallTraceKey{}, ModelCallTrace{attempt, spanID})
}
func ModelCallTraceFromContext(ctx context.Context) ModelCallTrace {
	trace, _ := ctx.Value(modelCallTraceKey{}).(ModelCallTrace)
	return trace
}

type modelCallImageKey struct{}
type ModelCallImageReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func WithModelCallImages(ctx context.Context, images []ModelCallImageReference) context.Context {
	return context.WithValue(ctx, modelCallImageKey{}, images)
}
func ModelCallImagesFromContext(ctx context.Context) []ModelCallImageReference {
	images, _ := ctx.Value(modelCallImageKey{}).([]ModelCallImageReference)
	return images
}
