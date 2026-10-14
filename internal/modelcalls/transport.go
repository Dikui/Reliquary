// Package modelcalls records actual outbound inference requests without storing content or credentials.
package modelcalls

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

type Transport struct{ Inner http.RoundTripper }

func WrapClient(client *http.Client) *http.Client {
	copy := *client
	inner := copy.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	if _, ok := inner.(*Transport); !ok {
		copy.Transport = &Transport{Inner: inner}
	}
	return &copy
}
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	session := types.ModelCallSessionFromContext(req.Context())
	if session == nil || session.Accounting.Model == nil || session.Accounting.Write == nil || req.Method != http.MethodPost {
		return t.Inner.RoundTrip(req)
	}
	m := session.Accounting.Model
	// Restrict control filtering to Ollama; remote providers also use /api/ paths.
	if m.Source == types.ModelSourceLocal && m.Type != types.ModelTypeRerank && !strings.HasSuffix(req.URL.Path, "/api/chat") && !strings.HasSuffix(req.URL.Path, "/api/embed") && !strings.HasSuffix(req.URL.Path, "/api/embeddings") && !strings.HasSuffix(req.URL.Path, "/api/generate") {
		return t.Inner.RoundTrip(req)
	}
	stage, _ := types.LLMCallMetadataFromContext(req.Context())
	space, _ := types.SpaceIDFromContext(req.Context())
	if space == 0 {
		space = m.SpaceID
	}
	provider := m.Parameters.Provider
	if provider == "" {
		provider = string(m.Source)
	}
	currency := m.Parameters.Pricing.Currency
	if currency == "" {
		currency = "USD"
	}
	call := &types.ModelCall{ID: uuid.NewString(), SpaceID: space, RequestedAt: time.Now().UTC(), ModelID: m.ID, ModelName: m.Name, Provider: provider,
		Purpose: types.NormalizeModelCallPurpose(stage, types.ModelTierFromContext(req.Context())), EntryID: types.ModelCallEntryFromContext(req.Context()),
		InputPrice: clonePrice(m.Parameters.Pricing.InputPerMillion), OutputPrice: clonePrice(m.Parameters.Pricing.OutputPerMillion), Currency: currency, CostStatus: "unknown",
		Attempt: int(session.Attempts.Add(1)), RetryNumber: types.ModelCallRetryFromContext(req.Context())}
	if m.Source == types.ModelSourceChatGPT {
		call.CostStatus = "not_applicable"
	}
	resp, err := t.Inner.RoundTrip(req)
	body := &accountingBody{ctx: req.Context(), call: call, write: session.Accounting.Write}
	if err != nil {
		call.ErrorCategory = "transport"
		if errors.Is(err, context.DeadlineExceeded) {
			call.ErrorCategory = "timeout"
		}
		body.finish(err)
		return resp, err
	}
	call.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !call.Success {
		call.ErrorCategory = httpErrorCategory(resp.StatusCode)
	}
	body.inner = resp.Body
	body.stream = strings.Contains(resp.Header.Get("Content-Type"), "event-stream") || strings.Contains(resp.Header.Get("Content-Type"), "ndjson")
	resp.Body = body
	// Cancellation can occur while a stream producer is blocked sending a chunk.
	// Persist the interrupted request even if it never reads or closes again.
	body.mu.Lock()
	body.stopCancel = context.AfterFunc(req.Context(), func() { body.finish(req.Context().Err()) })
	body.mu.Unlock()
	return resp, nil
}
func clonePrice(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func httpErrorCategory(code int) string {
	switch {
	case code == 401 || code == 403:
		return "authentication"
	case code == 429:
		return "rate_limit"
	case code >= 500:
		return "provider"
	default:
		return "http"
	}
}

type accountingBody struct {
	inner      io.ReadCloser
	ctx        context.Context
	call       *types.ModelCall
	write      func(context.Context, *types.ModelCall) error
	once       sync.Once
	mu         sync.Mutex
	finished   bool
	stopCancel func() bool
	pending    []byte
	stream     bool
	terminal   bool
	eof        bool
	invalid    bool
	validJSON  bool
}

const maxAccountingEvent = 8 * 1024 * 1024

func (b *accountingBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	b.mu.Lock()
	if !b.finished {
		if n > 0 {
			if len(b.pending)+n <= maxAccountingEvent {
				b.pending = append(b.pending, p[:n]...)
			} else {
				b.pending = nil
				b.invalid = true
			}
			if b.stream {
				b.consumeLines(false)
			}
		}
		if err != nil {
			b.eof = err == io.EOF
			if b.stream {
				b.consumeLines(true)
			} else {
				b.inspect(b.pending)
				b.pending = nil
			}
		}
	}
	b.mu.Unlock()
	if err != nil {
		b.finish(err)
	}
	return n, err
}
func (b *accountingBody) Close() error {
	err := b.inner.Close()
	b.mu.Lock()
	if !b.finished {
		if b.stream {
			b.consumeLines(true)
		} else if !b.eof {
			b.inspect(b.pending)
		}
	}
	b.mu.Unlock()
	b.finish(err)
	return err
}
func (b *accountingBody) consumeLines(final bool) {
	for {
		i := bytes.IndexByte(b.pending, '\n')
		if i < 0 {
			break
		}
		line := b.pending[:i]
		b.inspectLine(line)
		b.pending = b.pending[i+1:]
	}
	if final && len(b.pending) > 0 {
		b.inspectLine(b.pending)
		b.pending = nil
	}
}
func (b *accountingBody) inspectLine(line []byte) {
	line = bytes.TrimSpace(line)
	if bytes.HasPrefix(line, []byte("data:")) {
		line = bytes.TrimSpace(line[5:])
	} else if !bytes.HasPrefix(line, []byte("{")) {
		return
	}
	if bytes.Equal(line, []byte("[DONE]")) {
		b.terminal = true
		return
	}
	b.inspect(line)
}
func (b *accountingBody) inspect(data []byte) {
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		b.invalid = true
		return
	}
	b.validJSON = true
	if raw, ok := obj["usage"]; ok && string(raw) != "null" {
		b.usage(raw)
	}
	// Anthropic message_start and Responses completion nest usage.
	for _, key := range []string{"message", "response"} {
		if raw, ok := obj[key]; ok {
			b.inspect(raw)
		}
	}
	if raw, ok := obj["prompt_eval_count"]; ok {
		b.call.InputTokens = token(raw)
	}
	if raw, ok := obj["eval_count"]; ok {
		b.call.OutputTokens = token(raw)
	}
	if string(obj["done"]) == "true" {
		b.terminal = true
	}
	var event string
	_ = json.Unmarshal(obj["type"], &event)
	if event == "message_stop" || event == "response.completed" {
		b.terminal = true
	}
	if event == "error" || event == "response.failed" || event == "response.incomplete" || (obj["error"] != nil && string(obj["error"]) != "null") {
		b.call.Success = false
		if b.call.ErrorCategory == "" {
			b.call.ErrorCategory = "provider"
		}
		b.terminal = true
	}
}
func token(raw json.RawMessage) *int64 {
	var n int64
	if json.Unmarshal(raw, &n) != nil || n < 0 {
		return nil
	}
	return &n
}
func (b *accountingBody) usage(raw json.RawMessage) {
	var u map[string]json.RawMessage
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	for _, key := range []string{"prompt_tokens", "input_tokens"} {
		if v, ok := u[key]; ok {
			b.call.InputTokens = token(v)
			// Native Anthropic input_tokens excludes cache reads and writes.
			if key == "input_tokens" && b.call.InputTokens != nil {
				n := *b.call.InputTokens
				for _, cacheKey := range []string{"cache_read_input_tokens", "cache_creation_input_tokens"} {
					if cached := token(u[cacheKey]); cached != nil {
						n += *cached
					}
				}
				b.call.InputTokens = &n
			}
		}
	}
	for _, key := range []string{"completion_tokens", "output_tokens"} {
		if v, ok := u[key]; ok {
			b.call.OutputTokens = token(v)
		}
	}
	// Input-only APIs sometimes report only total_tokens.
	if b.call.InputTokens == nil && b.call.OutputTokens == nil && (types.ModelTierFromContext(b.ctx) == types.ModelTierEmbedding || types.ModelTierFromContext(b.ctx) == types.ModelTierRerank) {
		if v, ok := u["total_tokens"]; ok {
			b.call.InputTokens = token(v)
		}
	}
}
func (b *accountingBody) finish(err error) {
	b.once.Do(func() {
		b.mu.Lock()
		b.finished = true
		snapshot := *b.call
		c := &snapshot
		if err != nil && !errors.Is(err, io.EOF) && !(b.stream && b.terminal) {
			c.Success = false
			c.ErrorCategory = "transport"
			if errors.Is(err, context.DeadlineExceeded) {
				c.ErrorCategory = "timeout"
			}
		}
		if b.stream && !b.terminal && c.ErrorCategory == "" {
			c.Success = false
			c.ErrorCategory = "interrupted"
		}
		if b.stream && !b.terminal && (c.ErrorCategory == "transport" || c.ErrorCategory == "timeout") {
			c.ErrorCategory = "interrupted"
		}
		if b.stream && c.Success && b.invalid {
			c.Success = false
			c.ErrorCategory = "invalid_response"
		}
		if !b.stream && c.Success && (!b.validJSON || b.invalid) {
			c.Success = false
			c.ErrorCategory = "invalid_response"
		}
		if c.CostStatus != "not_applicable" && c.InputTokens != nil && c.InputPrice != nil {
			cost := float64(*c.InputTokens) * (*c.InputPrice) / 1e6
			inputOnly := types.ModelTierFromContext(b.ctx) == types.ModelTierEmbedding || types.ModelTierFromContext(b.ctx) == types.ModelTierRerank
			if c.OutputTokens != nil && c.OutputPrice != nil {
				cost += float64(*c.OutputTokens) * (*c.OutputPrice) / 1e6
				c.Cost = &cost
				c.CostStatus = "calculated"
			} else if inputOnly && c.OutputTokens == nil {
				c.Cost = &cost
				c.CostStatus = "calculated"
			}
		}
		c.DurationMS = time.Since(c.RequestedAt).Milliseconds()
		b.pending = nil
		if b.stopCancel != nil {
			b.stopCancel()
		}
		b.mu.Unlock()
		// Detached from business cancellation, bounded to one second, no business error propagation.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(b.ctx), time.Second)
		defer cancel()
		if writeErr := b.write(ctx, c); writeErr != nil {
			logger.Warnf(ctx, "model call accounting failed: %v", writeErr)
		}
	})
}
