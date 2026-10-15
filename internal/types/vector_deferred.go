package types

import (
	"fmt"
	"time"
)

// DeferredOperation is understood by the durable pending operation queue.
type DeferredOperation interface {
	error
	DeferredUntil() time.Time
}
type VectorDeferredError struct {
	Cause   error
	RetryAt time.Time
}

func (e *VectorDeferredError) Error() string {
	return fmt.Sprintf("vector unavailable; retry later: %v", e.Cause)
}
func (e *VectorDeferredError) Unwrap() error             { return e.Cause }
func (e *VectorDeferredError) DeferredUntil() time.Time  { return e.RetryAt }
func (e *ChatGPTDeferredError) DeferredUntil() time.Time { return e.RetryAt }
